package main

import (
	"context"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Parse the HTML template for the main page
var pageTemplate = template.Must(template.ParseFiles("templates/index.html"))

type PageData struct {
	URL    string
	Error  string
	Result *AnalysisResult
}

var analyzer = NewAnalyzer(&http.Client{Timeout: 8 * time.Second}, 24)

func homeHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{}

	if r.Method == http.MethodPost {
		data.URL = strings.TrimSpace(r.FormValue("url"))
		if _, err := validateWebURL(data.URL); err != nil {
			data.Error = err.Error()
		} else {
			analysisContext, cancel := context.WithTimeout(r.Context(), 45*time.Second)
			defer cancel()

			result, err := analyzer.Analyze(analysisContext, data.URL)
			if err != nil {
				data.Error = err.Error()
			} else {
				data.Result = result
			}
		}
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := pageTemplate.Execute(w, data)
	if err != nil {
		slog.Error("Failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/", homeHandler)

	server := &http.Server{
		Addr:              ":8080",
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("Starting server", "address", "http://localhost:8080")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownContext); err != nil {
		slog.Error("Server failed", "error", err)
	}
}
