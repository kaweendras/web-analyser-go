package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"web-analyser-go/internal/analyzer"
)

const maxFormBodySize = 1 << 20 // 1 MiB

//go:embed templates/index.html
var templateFiles embed.FS

var pageTemplate = template.Must(template.ParseFS(templateFiles, "templates/index.html"))

// PageAnalyzer describes the application behavior required by the HTTP layer.
type PageAnalyzer interface {
	Analyze(context.Context, string) (*analyzer.Result, error)
	AnalyzeWithProgress(context.Context, string, analyzer.ProgressFunc) (*analyzer.Result, error)
}

// Handler serves the web interface and streams analysis progress and results.
type Handler struct {
	analyzer PageAnalyzer
	timeout  time.Duration
}

type pageData struct {
	URL    string
	Error  string
	Result *analyzer.Result
}

type streamEvent struct {
	Type      string           `json:"type"`
	Message   string           `json:"message,omitempty"`
	Completed *int             `json:"completed,omitempty"`
	Total     *int             `json:"total,omitempty"`
	Result    *analyzer.Result `json:"result,omitempty"`
}

// NewHandler creates the application's HTTP handler.
func NewHandler(pageAnalyzer PageAnalyzer, timeout time.Duration) *Handler {
	return &Handler{analyzer: pageAnalyzer, timeout: timeout}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/":
		h.servePage(w, r)
	case "/api/analyze":
		h.streamAnalysis(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) servePage(w http.ResponseWriter, r *http.Request) {
	data := pageData{}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		submittedURL, ok := parseSubmittedURL(w, r)
		if !ok {
			return
		}
		data.URL = submittedURL
		analysisContext, cancel := context.WithTimeout(r.Context(), h.timeout)
		defer cancel()

		result, err := h.analyzer.Analyze(analysisContext, data.URL)
		data.Result = result
		if err != nil {
			data.Error = analysisMessage(err, result != nil)
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var rendered bytes.Buffer
	if err := pageTemplate.ExecuteTemplate(&rendered, "index.html", data); err != nil {
		slog.Error("Failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := w.Write(rendered.Bytes()); err != nil {
		slog.Warn("Failed to write rendered page", "error", err)
	}
}

func (h *Handler) streamAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	submittedURL, ok := parseSubmittedURL(w, r)
	if !ok {
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming is not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	encoder := json.NewEncoder(w)
	send := func(event streamEvent) error {
		if err := encoder.Encode(event); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	analysisContext, cancel := context.WithTimeout(r.Context(), h.timeout)
	defer cancel()
	if err := send(streamEvent{Type: "status", Message: "Fetching and parsing the page"}); err != nil {
		return
	}

	var streamErr error
	lastProgressSent := -1
	latestCompleted := 0
	latestTotal := 0
	hasProgress := false
	progress := func(completed, total int) {
		if streamErr != nil {
			return
		}
		latestCompleted = completed
		latestTotal = total
		hasProgress = true
		if total > 100 && completed > 0 && completed < total {
			step := total / 100
			if completed-lastProgressSent < step {
				return
			}
		}
		lastProgressSent = completed
		streamErr = send(streamEvent{
			Type:      "progress",
			Completed: intValue(completed),
			Total:     intValue(total),
		})
		if streamErr != nil {
			cancel()
		}
	}

	result, err := h.analyzer.AnalyzeWithProgress(
		analysisContext,
		submittedURL,
		progress,
	)
	if streamErr != nil {
		return
	}
	if err != nil {
		eventType := "error"
		if result != nil {
			eventType = "partial"
		}
		event := streamEvent{
			Type:    eventType,
			Message: analysisMessage(err, result != nil),
			Result:  result,
		}
		if result != nil && hasProgress {
			event.Completed = intValue(latestCompleted)
			event.Total = intValue(latestTotal)
		}
		_ = send(event)
		return
	}
	_ = send(streamEvent{Type: "result", Result: result})
}

func parseSubmittedURL(w http.ResponseWriter, r *http.Request) (string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodySize)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return "", false
	}
	return strings.TrimSpace(r.Form.Get("url")), true
}

func intValue(value int) *int {
	return &value
}

func analysisMessage(err error, hasPartialResult bool) string {
	if errors.Is(err, context.DeadlineExceeded) {
		if hasPartialResult {
			return "Analysis timed out before every link could be checked. Here is what was analyzed before the timeout."
		}
		return "Analysis timed out before the page could be analyzed. Please try again."
	}
	return err.Error()
}
