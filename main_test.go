package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHomeHandlerDisplaysForm(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()

	homeHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), "Web Page Analyzer") {
		t.Error("response does not contain the page heading")
	}
}

func TestHomeHandlerRejectsInvalidURL(t *testing.T) {
	form := url.Values{"url": {"not-a-url"}}
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response := httptest.NewRecorder()

	homeHandler(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !strings.Contains(response.Body.String(), "enter a valid URL") {
		t.Error("response does not contain the validation error")
	}
}

func TestHomeHandlerRejectsUnsupportedMethod(t *testing.T) {
	request := httptest.NewRequest(http.MethodDelete, "/", nil)
	response := httptest.NewRecorder()

	homeHandler(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
	if response.Header().Get("Allow") != "GET, POST" {
		t.Errorf("Allow header = %q, want GET, POST", response.Header().Get("Allow"))
	}
}
