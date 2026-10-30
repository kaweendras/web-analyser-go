package web

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"web-analyser-go/internal/analyzer"
	"web-analyser-go/internal/linkchecker"
)

func TestAnalysisTimeoutStreamsPartialResults(t *testing.T) {
	linkServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/fast" {
			response.WriteHeader(http.StatusOK)
			return
		}
		<-request.Context().Done()
	}))
	defer linkServer.Close()

	pageBody := fmt.Sprintf(`<!doctype html><html><head><title>Partial test</title></head><body>
		<h1>Test heading</h1>
		<a href="%s/fast">fast</a>
		<a href="%s/slow-one">slow one</a>
		<a href="%s/slow-two">slow two</a>
	</body></html>`, linkServer.URL, linkServer.URL, linkServer.URL)
	pageServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(response, pageBody)
	}))
	defer pageServer.Close()

	client := &http.Client{Timeout: time.Second}
	pageAnalyzer := analyzer.New(client, linkchecker.New(client, 1))
	handler := NewHandler(pageAnalyzer, 75*time.Millisecond)
	form := url.Values{"url": {pageServer.URL}}
	request := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	var partial streamEvent
	var lastProgress streamEvent
	scanner := bufio.NewScanner(response.Body)
	for scanner.Scan() {
		var event streamEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("decode stream event: %v", err)
		}
		switch event.Type {
		case "progress":
			lastProgress = event
		case "partial":
			partial = event
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	if partial.Result == nil {
		t.Fatal("stream does not contain a partial result")
	}
	if partial.Result.Title != "Partial test" || partial.Result.Headings[0] != 1 {
		t.Errorf("partial page data = %+v, want parsed title and H1", partial.Result)
	}
	if partial.Result.CheckedLinks < 1 || partial.Result.CheckedLinks >= 3 {
		t.Errorf("checked links = %d, want some but not all 3 links", partial.Result.CheckedLinks)
	}
	if lastProgress.Completed == nil || lastProgress.Total == nil {
		t.Fatal("last progress event is missing completed or total")
	}
	if *lastProgress.Completed != partial.Result.CheckedLinks || *lastProgress.Total != 3 {
		t.Errorf("last progress = %d/%d, partial checked links = %d", *lastProgress.Completed, *lastProgress.Total, partial.Result.CheckedLinks)
	}
	if partial.Completed == nil || partial.Total == nil {
		t.Fatal("partial event is missing its latest progress")
	}
	if *partial.Completed != partial.Result.CheckedLinks || *partial.Total != 3 {
		t.Errorf("partial progress = %d/%d, checked links = %d", *partial.Completed, *partial.Total, partial.Result.CheckedLinks)
	}
	if !strings.Contains(partial.Message, "Here is what was analyzed before the timeout.") {
		t.Errorf("partial message = %q", partial.Message)
	}
}
