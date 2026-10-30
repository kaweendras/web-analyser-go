package analyzer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/net/html"

	"web-analyser-go/internal/linkchecker"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func response(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

type fakeChecker struct {
	links   []string
	results []linkchecker.Result
	err     error
}

func (f *fakeChecker) Check(_ context.Context, links []string, progress linkchecker.ProgressFunc) ([]linkchecker.Result, error) {
	f.links = append([]string(nil), links...)
	if progress != nil {
		progress(len(links), len(links))
	}
	return f.results, f.err
}

func TestAnalyzePage(t *testing.T) {
	body := `<!doctype html><html><head><title> Test Page </title></head><body>
		<h1>Heading</h1><h2>Subheading</h2>
		<form><input type="password"></form>
		<a href="/inside">inside</a><a href="/inside">duplicate</a>
		<a href="https://outside.test/page">outside</a>
	</body></html>`
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		return response(request, http.StatusOK, body), nil
	})
	checker := &fakeChecker{results: []linkchecker.Result{
		{URL: "https://example.com/inside", StatusCode: http.StatusOK},
		{URL: "https://outside.test/page", StatusCode: http.StatusNotFound},
	}}

	progressCompleted := 0
	progressTotal := 0
	result, err := New(&http.Client{Transport: transport}, checker).
		AnalyzeWithProgress(context.Background(), "https://example.com", func(completed, total int) {
			progressCompleted = completed
			progressTotal = total
		})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.HTMLVersion != "HTML5" || result.Title != "Test Page" {
		t.Errorf("version/title = %q/%q", result.HTMLVersion, result.Title)
	}
	if result.Headings[0] != 1 || result.Headings[1] != 1 {
		t.Errorf("headings = %v, want one H1 and one H2", result.Headings)
	}
	if !result.HasLoginForm {
		t.Error("HasLoginForm = false, want true")
	}
	if result.InternalLinks != 2 || result.ExternalLinks != 1 {
		t.Errorf("link occurrences = %d internal/%d external", result.InternalLinks, result.ExternalLinks)
	}
	if result.CheckedLinks != 2 || len(checker.links) != 2 {
		t.Errorf("unique checks = %d and checker links = %d, want 2", result.CheckedLinks, len(checker.links))
	}
	if result.InaccessibleLinks != 1 {
		t.Errorf("inaccessible links = %d, want 1", result.InaccessibleLinks)
	}
	if progressCompleted != 2 || progressTotal != 2 {
		t.Errorf("progress = %d/%d, want 2/2", progressCompleted, progressTotal)
	}
}

func TestValidateWebURL(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{value: "https://example.com/path"},
		{value: "http://example.com"},
		{value: "example.com", wantErr: true},
		{value: "https:///path", wantErr: true},
		{value: "ftp://example.com", wantErr: true},
	}
	for _, test := range tests {
		_, err := validateWebURL(test.value)
		if (err != nil) != test.wantErr {
			t.Errorf("validateWebURL(%q) error = %v, wantErr %v", test.value, err, test.wantErr)
		}
	}
}

func TestDetectHTMLVersion(t *testing.T) {
	tests := []struct {
		declaration string
		want        string
	}{
		{declaration: "html", want: "HTML5"},
		{declaration: `html public "-//W3C//DTD HTML 4.01//EN"`, want: "HTML 4.01"},
		{declaration: `html public "-//W3C//DTD XHTML 1.0 Strict//EN"`, want: "XHTML 1.0"},
	}
	for _, test := range tests {
		parts := strings.SplitN(test.declaration, " ", 2)
		node := &html.Node{Type: html.DoctypeNode, Data: parts[0]}
		if len(parts) == 2 {
			node.Attr = []html.Attribute{{Key: "public", Val: parts[1]}}
		}
		if got := detectHTMLVersion(node); got != test.want {
			t.Errorf("detectHTMLVersion() = %q, want %q", got, test.want)
		}
	}
}

func TestResolveWebLink(t *testing.T) {
	baseURL, _ := url.Parse("https://example.com/articles/page")
	resolved, ok := resolveWebLink(baseURL, "../about#team")
	if !ok || resolved.String() != "https://example.com/about" {
		t.Fatalf("resolveWebLink() = %v, %v", resolved, ok)
	}
	for _, value := range []string{"", "#section", "mailto:person@example.com", "://bad"} {
		if _, ok := resolveWebLink(baseURL, value); ok {
			t.Errorf("resolveWebLink(%q) accepted a non-web link", value)
		}
	}
}

func TestAnalyzeReportsFetchAndCheckerErrors(t *testing.T) {
	t.Run("HTTP error", func(t *testing.T) {
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return response(request, http.StatusServiceUnavailable, ""), nil
		})
		_, err := New(&http.Client{Transport: transport}, &fakeChecker{}).
			Analyze(context.Background(), "https://example.com")
		var fetchError *FetchError
		if !errors.As(err, &fetchError) || fetchError.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("error = %v, want FetchError with status 503", err)
		}
		if got, want := err.Error(), "HTTP status 503: Service Unavailable"; got != want {
			t.Errorf("error message = %q, want %q", got, want)
		}
	})

	t.Run("link checker error", func(t *testing.T) {
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return response(request, http.StatusOK, `<!doctype html><a href="/page">page</a>`), nil
		})
		checker := &fakeChecker{err: context.Canceled}
		_, err := New(&http.Client{Transport: transport}, checker).
			Analyze(context.Background(), "https://example.com")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
}
