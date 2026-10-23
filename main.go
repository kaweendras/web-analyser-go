package main

import (
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
)

// Parse the HTML template for the main page
var pageTemplate = template.Must(template.ParseFiles("templates/index.html"))

type PageData struct {
	URL   string
	Error string
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	data := PageData{}

	if r.Method == http.MethodPost {
		data.URL = r.FormValue("url")

		parsedURL, parseErr := url.ParseRequestURI(data.URL)
		if parseErr != nil ||
			parsedURL.Host == "" ||
			(parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			data.Error = "Please enter a valid HTTP or HTTPS URL."
		}
	}

	err := pageTemplate.Execute(w, data)
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
