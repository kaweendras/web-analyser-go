package web

import (
	"context"
	"embed"
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

// NewHandler creates the application's HTTP handler.
func NewHandler(pageAnalyzer PageAnalyzer, timeout time.Duration) *Handler {
	return &Handler{analyzer: pageAnalyzer, timeout: timeout}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	data := pageData{}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPost:
		data.URL = strings.TrimSpace(r.FormValue("url"))
		analysisContext, cancel := context.WithTimeout(r.Context(), h.timeout)
		defer cancel()

		result, err := h.analyzer.Analyze(analysisContext, data.URL)
		if err != nil {
			data.Error = err.Error()
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
