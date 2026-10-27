package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"web-analyser-go/internal/analyzer"
	"web-analyser-go/internal/linkchecker"
	"web-analyser-go/internal/web"
)

const (
	maxLinkWorkers = 20
	requestTimeout = 8 * time.Second
	analysisLimit  = 2 * time.Minute
)

func main() {
	client := &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: maxLinkWorkers,
			IdleConnTimeout:     90 * time.Second,
		},
	}
	checker := linkchecker.New(client, maxLinkWorkers)
	pageAnalyzer := analyzer.New(client, checker)

	mux := http.NewServeMux()
	mux.Handle("/", web.NewHandler(pageAnalyzer, analysisLimit))

	server := &http.Server{
		Addr:              ":8080",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      analysisLimit + 15*time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		slog.Info("Starting server", "address", "http://localhost:8080", "max_link_workers", maxLinkWorkers)
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
		slog.Error("Server shutdown failed", "error", err)
	}
}
