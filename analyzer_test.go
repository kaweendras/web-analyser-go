package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/html"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func testResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestValidateWebURL(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "https", value: "https://example.com/path"},
		{name: "http", value: "http://example.com"},
		{name: "trims spaces", value: "  https://example.com  "},
		{name: "missing scheme", value: "example.com", wantErr: true},
		{name: "missing host", value: "https:///path", wantErr: true},
		{name: "unsupported scheme", value: "ftp://example.com", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateWebURL(test.value)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateWebURL() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestDetectHTMLVersion(t *testing.T) {
	tests := []struct {
		name        string
		declaration string
		want        string
	}{
		{name: "HTML5", declaration: "html", want: "HTML5"},
		{name: "HTML 4", declaration: `html public "-//W3C//DTD HTML 4.01//EN"`, want: "HTML 4.01"},
		{name: "XHTML", declaration: `html public "-//W3C//DTD XHTML 1.0 Strict//EN"`, want: "XHTML 1.0"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			parts := strings.SplitN(test.declaration, " ", 2)
			node := &html.Node{Type: html.DoctypeNode, Data: parts[0]}
			if len(parts) == 2 {
				node.Attr = []html.Attribute{{Key: "public", Val: parts[1]}}
			}
			if got := detectHTMLVersion(node); got != test.want {
				t.Errorf("detectHTMLVersion() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestResolveWebLink(t *testing.T) {
	baseURL, err := url.Parse("https://example.com/articles/page")
	if err != nil {
		t.Fatal(err)
	}

	resolved, ok := resolveWebLink(baseURL, "../about#team")
	if !ok || resolved.String() != "https://example.com/about" {
		t.Fatalf("resolveWebLink() = %v, %v; want https://example.com/about, true", resolved, ok)
	}

	ignored := []string{"", "#section", "mailto:person@example.com", "://bad"}
	for _, value := range ignored {
		if _, ok := resolveWebLink(baseURL, value); ok {
			t.Errorf("resolveWebLink(%q) accepted a non-web link", value)
		}
	}
}

func TestAnalyzeLargePageUsesBoundedConcurrency(t *testing.T) {
	const (
		internalLinks = 300
		externalLinks = 150
		workerCount   = 24
	)

	var page strings.Builder
	page.WriteString("<!doctype html><html><head><title> Large Test Page </title></head><body>")
	page.WriteString("<h1>One</h1><h2>Two</h2><form><input type=\"password\"></form>")
	brokenLinks := 0
	for index := 0; index < internalLinks+externalLinks; index++ {
		host := ""
		if index >= internalLinks {
			host = "https://external.test"
		}
		path := "ok"
		if index%50 == 0 {
			path = "broken"
			brokenLinks++
		}
		fmt.Fprintf(&page, `<a href="%s/%s/%d">link</a>`, host, path, index)
	}
	page.WriteString("</body></html>")

	var active atomic.Int32
	var maximum atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.String() == "https://example.com" {
			return testResponse(request, http.StatusOK, page.String()), nil
		}

		current := active.Add(1)
		defer active.Add(-1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		time.Sleep(time.Millisecond)

		status := http.StatusOK
		if strings.Contains(request.URL.Path, "/broken/") {
			status = http.StatusNotFound
		}
		return testResponse(request, status, ""), nil
	})

	client := &http.Client{Transport: transport}
	result, err := NewAnalyzer(client, workerCount).Analyze(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if result.HTMLVersion != "HTML5" {
		t.Errorf("HTMLVersion = %q, want HTML5", result.HTMLVersion)
	}
	if result.Title != "Large Test Page" {
		t.Errorf("Title = %q, want Large Test Page", result.Title)
	}
	if result.Headings[0] != 1 || result.Headings[1] != 1 {
		t.Errorf("Headings = %v, want one H1 and one H2", result.Headings)
	}
	if !result.HasLoginForm {
		t.Error("HasLoginForm = false, want true")
	}
	if result.InternalLinks != internalLinks {
		t.Errorf("InternalLinks = %d, want %d", result.InternalLinks, internalLinks)
	}
	if result.ExternalLinks != externalLinks {
		t.Errorf("ExternalLinks = %d, want %d", result.ExternalLinks, externalLinks)
	}
	if result.CheckedLinks != internalLinks+externalLinks {
		t.Errorf("CheckedLinks = %d, want %d", result.CheckedLinks, internalLinks+externalLinks)
	}
	if result.InaccessibleLinks != brokenLinks {
		t.Errorf("InaccessibleLinks = %d, want %d", result.InaccessibleLinks, brokenLinks)
	}
	if maximum.Load() > workerCount {
		t.Errorf("maximum concurrency = %d, want at most %d", maximum.Load(), workerCount)
	}
	if maximum.Load() < 2 {
		t.Errorf("maximum concurrency = %d, expected concurrent checks", maximum.Load())
	}
}

func TestAnalyzeDeduplicatesChecksButCountsOccurrences(t *testing.T) {
	htmlBody := `<!doctype html><a href="/same">first</a><a href="/same">second</a>`
	var checks atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodGet && request.URL.String() == "https://example.com" {
			return testResponse(request, http.StatusOK, htmlBody), nil
		}
		checks.Add(1)
		return testResponse(request, http.StatusOK, ""), nil
	})

	result, err := NewAnalyzer(&http.Client{Transport: transport}, 4).
		Analyze(context.Background(), "https://example.com")
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if result.InternalLinks != 2 {
		t.Errorf("InternalLinks = %d, want 2 occurrences", result.InternalLinks)
	}
	if result.CheckedLinks != 1 || checks.Load() != 1 {
		t.Errorf("unique checks = %d and requests = %d, want 1", result.CheckedLinks, checks.Load())
	}
}

func TestLinkCheckFallsBackToGet(t *testing.T) {
	var methods []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		methods = append(methods, request.Method)
		if request.Method == http.MethodHead {
			return testResponse(request, http.StatusMethodNotAllowed, ""), nil
		}
		return testResponse(request, http.StatusOK, ""), nil
	})

	analyzer := NewAnalyzer(&http.Client{Transport: transport}, 1)
	if !analyzer.linkIsAccessible(context.Background(), "https://example.com/page") {
		t.Fatal("linkIsAccessible() = false, want true")
	}
	if strings.Join(methods, ",") != "HEAD,GET" {
		t.Errorf("methods = %v, want HEAD then GET", methods)
	}
}

func TestAnalyzeReportsHTTPAndNetworkErrors(t *testing.T) {
	t.Run("HTTP status", func(t *testing.T) {
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return testResponse(request, http.StatusServiceUnavailable, ""), nil
		})
		_, err := NewAnalyzer(&http.Client{Transport: transport}, 1).
			Analyze(context.Background(), "https://example.com")

		var fetchError *FetchError
		if !errors.As(err, &fetchError) || fetchError.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("error = %v, want FetchError with status 503", err)
		}
	})

	t.Run("network error", func(t *testing.T) {
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			return nil, errors.New("network unavailable")
		})
		_, err := NewAnalyzer(&http.Client{Transport: transport}, 1).
			Analyze(context.Background(), "https://example.com")

		var fetchError *FetchError
		if !errors.As(err, &fetchError) || fetchError.StatusCode != 0 {
			t.Fatalf("error = %v, want FetchError without status", err)
		}
	})
}

func TestCountInaccessibleLinksHonorsCancellation(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := NewAnalyzer(&http.Client{Transport: transport}, 2).
		countInaccessibleLinks(ctx, []string{"https://one.test", "https://two.test"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}
