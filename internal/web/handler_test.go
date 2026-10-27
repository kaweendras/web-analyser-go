package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"web-analyser-go/internal/analyzer"
)

func TestStreamEventIncludesZeroProgressValues(t *testing.T) {
	encoded, err := json.Marshal(streamEvent{Type: "progress"})
	if err != nil {
		t.Fatal(err)
	}
	body := string(encoded)
	for _, expected := range []string{`"completed":0`, `"total":0`} {
		if !strings.Contains(body, expected) {
			t.Errorf("zero-link progress event does not contain %s: %s", expected, body)
		}
	}
}

type fakeAnalyzer struct {
	result *analyzer.Result
	err    error
	url    string
}

func (f *fakeAnalyzer) Analyze(_ context.Context, submittedURL string) (*analyzer.Result, error) {
	f.url = submittedURL
	return f.result, f.err
}

func (f *fakeAnalyzer) AnalyzeWithProgress(_ context.Context, submittedURL string, progress analyzer.ProgressFunc) (*analyzer.Result, error) {
	f.url = submittedURL
	if progress != nil {
		progress(1, 2)
		progress(2, 2)
	}
	return f.result, f.err
}

func TestHandlerDisplaysForm(t *testing.T) {
	handler := NewHandler(&fakeAnalyzer{}, time.Second)
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Web Page Analyzer") {
		t.Error("response does not contain the page heading")
	}
}

func TestHandlerDisplaysResults(t *testing.T) {
	pageAnalyzer := &fakeAnalyzer{result: &analyzer.Result{
		URL:           "https://example.com/final",
		HTMLVersion:   "HTML5",
		Title:         "Example",
		InternalLinks: 2,
	}}
	handler := NewHandler(pageAnalyzer, time.Second)
	form := url.Values{"url": {"  https://example.com  "}}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if pageAnalyzer.url != "https://example.com" {
		t.Errorf("submitted URL = %q, want trimmed URL", pageAnalyzer.url)
	}
	if !strings.Contains(response.Body.String(), "Analysis results") {
		t.Error("response does not contain analysis results")
	}
}

func TestHandlerDisplaysAnalyzerError(t *testing.T) {
	handler := NewHandler(&fakeAnalyzer{err: errors.New("invalid URL")}, time.Second)
	form := url.Values{"url": {"invalid"}}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !strings.Contains(response.Body.String(), "invalid URL") {
		t.Error("response does not contain analyzer error")
	}
}

func TestHandlerExplainsAnalysisTimeout(t *testing.T) {
	handler := NewHandler(&fakeAnalyzer{err: context.DeadlineExceeded}, time.Second)
	form := url.Values{"url": {"https://example.com"}}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if !strings.Contains(response.Body.String(), "Analysis timed out") {
		t.Error("response does not contain the friendly timeout message")
	}
}

func TestHandlerRejectsUnsupportedMethodAndPath(t *testing.T) {
	handler := NewHandler(&fakeAnalyzer{}, time.Second)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodDelete, "/", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("DELETE status = %d, want 405", response.Code)
	}

	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/missing", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing path status = %d, want 404", response.Code)
	}
}

func TestHandlerStreamsProgressAndResult(t *testing.T) {
	pageAnalyzer := &fakeAnalyzer{result: &analyzer.Result{URL: "https://example.com", Title: "Example"}}
	handler := NewHandler(pageAnalyzer, time.Second)
	form := url.Values{"url": {"https://livecricketsl.cc.nf/"}}
	request := httptest.NewRequest(http.MethodPost, "/api/analyze", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	body := response.Body.String()
	if pageAnalyzer.url != "https://livecricketsl.cc.nf/" {
		t.Errorf("streamed submitted URL = %q, want https://livecricketsl.cc.nf/", pageAnalyzer.url)
	}
	for _, expected := range []string{
		`"type":"status"`,
		`"type":"progress"`,
		`"completed":2`,
		`"total":2`,
		`"type":"result"`,
		`"title":"Example"`,
	} {
		if !strings.Contains(body, expected) {
			t.Errorf("stream does not contain %s\nbody: %s", expected, body)
		}
	}
}
