package web

import (
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

//go:embed templates/index.html
var templateFiles embed.FS

var pageTemplate = template.Must(template.ParseFS(templateFiles, "templates/index.html"))

// PageAnalyzer describes the application behavior required by the HTTP layer.
type PageAnalyzer interface {
	Analyze(context.Context, string) (*analyzer.Result, error)
	AnalyzeWithProgress(context.Context, string, analyzer.ProgressFunc) (*analyzer.Result, error)
}

// Handler serves the form and analysis results.
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
	Completed int              `json:"completed,omitempty"`
	Total     int              `json:"total,omitempty"`
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
		data.URL = strings.TrimSpace(r.FormValue("url"))
		analysisContext, cancel := context.WithTimeout(r.Context(), h.timeout)
		defer cancel()

		result, err := h.analyzer.Analyze(analysisContext, data.URL)
		if err != nil {
			data.Error = errorMessage(err)
		} else {
			data.Result = result
		}
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := pageTemplate.ExecuteTemplate(w, "index.html", data); err != nil {
		slog.Error("Failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func (h *Handler) streamAnalysis(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
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
	progress := func(completed, total int) {
		if streamErr != nil {
			return
		}
		if total > 100 && completed > 0 && completed < total {
			step := total / 100
			if completed-lastProgressSent < step {
				return
			}
		}
		lastProgressSent = completed
		streamErr = send(streamEvent{Type: "progress", Completed: completed, Total: total})
		if streamErr != nil {
			cancel()
		}
	}

	result, err := h.analyzer.AnalyzeWithProgress(
		analysisContext,
		strings.TrimSpace(r.FormValue("url")),
		progress,
	)
	if streamErr != nil {
		return
	}
	if err != nil {
		_ = send(streamEvent{Type: "error", Message: errorMessage(err)})
		return
	}
	_ = send(streamEvent{Type: "result", Result: result})
}

func errorMessage(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "Analysis timed out because some links responded too slowly. Please try again."
	}
	return err.Error()
}
