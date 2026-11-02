package web

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"web-analyser-go/internal/analyzer"
)

func TestClassifyAnalysisError(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		hasPartial bool
		wantCode   string
	}{
		{name: "invalid URL", err: &analyzer.ValidationError{Code: "invalid_url"}, wantCode: "invalid_url"},
		{name: "missing hostname", err: &analyzer.ValidationError{Code: "missing_hostname"}, wantCode: "missing_hostname"},
		{name: "unsupported scheme", err: &analyzer.ValidationError{Code: "unsupported_scheme"}, wantCode: "unsupported_scheme"},
		{name: "access denied", err: &analyzer.FetchError{StatusCode: http.StatusForbidden}, wantCode: "page_access_denied"},
		{name: "not found", err: &analyzer.FetchError{StatusCode: http.StatusNotFound}, wantCode: "page_not_found"},
		{name: "rate limited", err: &analyzer.FetchError{StatusCode: http.StatusTooManyRequests}, wantCode: "rate_limited"},
		{name: "website unavailable", err: &analyzer.FetchError{StatusCode: http.StatusServiceUnavailable}, wantCode: "page_unavailable"},
		{name: "connection timeout", err: &analyzer.FetchError{Err: context.DeadlineExceeded}, wantCode: "connection_timeout"},
		{name: "connection failed", err: &analyzer.FetchError{Err: errors.New("dial failed")}, wantCode: "connection_failed"},
		{name: "timeout", err: context.DeadlineExceeded, wantCode: "analysis_timeout"},
		{name: "partial timeout", err: context.DeadlineExceeded, hasPartial: true, wantCode: "partial_timeout"},
		{name: "unexpected", err: errors.New("private internal error"), wantCode: "analysis_failed"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := classifyAnalysisError(test.err, test.hasPartial)
			if got.Code != test.wantCode {
				t.Errorf("code = %q, want %q", got.Code, test.wantCode)
			}
			if got.Message == "" {
				t.Error("message is empty")
			}
			if got.Message == test.err.Error() {
				t.Errorf("message exposes the raw error: %q", got.Message)
			}
		})
	}
}
