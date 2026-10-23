package main

import (
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"net/url"
)

// Parse the HTML template for the main page
var pageTemplate = template.Must(template.ParseFiles("templates/index.html"))

func homeHandler(w http.ResponseWriter, r *http.Request) {
	err := pageTemplate.Execute(w, nil)
	if err != nil {
		slog.Error("Failed to render template", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

func main() {
	http.HandleFunc("/", homeHandler)
	slog.Info("Starting server on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
