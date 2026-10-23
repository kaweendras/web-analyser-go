package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

func homeHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Web Page Analyzer")
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
