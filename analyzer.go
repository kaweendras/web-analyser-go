package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

const maxPageSize = 10 << 20 // 10 MiB

type Analyzer struct {
	client  *http.Client
	workers int
}

type AnalysisResult struct {
	URL               string
	HTMLVersion       string
	Title             string
	Headings          [6]int
	InternalLinks     int
	ExternalLinks     int
	CheckedLinks      int
	InaccessibleLinks int
	HasLoginForm      bool
}

type FetchError struct {
	StatusCode int
	Err        error
}

func (e *FetchError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("HTTP status %d: %v", e.StatusCode, e.Err)
	}
	return fmt.Sprintf("HTTP status unavailable: %v", e.Err)
}

func (e *FetchError) Unwrap() error {
	return e.Err
}

func NewAnalyzer(client *http.Client, workers int) *Analyzer {
	if workers < 1 {
		workers = 1
	}
	return &Analyzer{client: client, workers: workers}
}

func validateWebURL(rawURL string) (*url.URL, error) {
	parsedURL, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil {
		return nil, errors.New("enter a valid URL")
	}
	if parsedURL.Host == "" {
		return nil, errors.New("URL must include a hostname")
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return nil, errors.New("URL must use HTTP or HTTPS")
	}
	return parsedURL, nil
}

func (a *Analyzer) Analyze(ctx context.Context, rawURL string) (*AnalysisResult, error) {
	requestedURL, err := validateWebURL(rawURL)
	if err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestedURL.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "WebAnalyzer/1.0")

	response, err := a.client.Do(request)
	if err != nil {
		return nil, &FetchError{Err: err}
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return nil, &FetchError{
			StatusCode: response.StatusCode,
			Err:        errors.New(response.Status),
		}
	}

	document, err := html.Parse(io.LimitReader(response.Body, maxPageSize))
	if err != nil {
		return nil, fmt.Errorf("parse HTML: %w", err)
	}

	pageURL := requestedURL
	if response.Request != nil && response.Request.URL != nil {
		pageURL = response.Request.URL
	}

	result, links := inspectDocument(document, pageURL)
	result.URL = pageURL.String()
	result.CheckedLinks = len(links)
	result.InaccessibleLinks, err = a.countInaccessibleLinks(ctx, links)
	if err != nil {
		return nil, fmt.Errorf("check links: %w", err)
	}

	return result, nil
}

func inspectDocument(document *html.Node, pageURL *url.URL) (*AnalysisResult, []string) {
	result := &AnalysisResult{HTMLVersion: "Unknown (no doctype)"}
	uniqueLinks := make(map[string]struct{})

	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.DoctypeNode {
			result.HTMLVersion = detectHTMLVersion(node)
		}

		if node.Type == html.ElementNode {
			switch node.Data {
			case "title":
				if result.Title == "" {
					result.Title = strings.TrimSpace(textContent(node))
				}
			case "h1", "h2", "h3", "h4", "h5", "h6":
				result.Headings[int(node.Data[1]-'1')]++
			case "form":
				if containsPasswordInput(node) {
					result.HasLoginForm = true
				}
			case "a":
				href, ok := attribute(node, "href")
				if !ok {
					break
				}
				resolved, ok := resolveWebLink(pageURL, href)
				if !ok {
					break
				}
				if strings.EqualFold(resolved.Hostname(), pageURL.Hostname()) {
					result.InternalLinks++
				} else {
					result.ExternalLinks++
				}
				uniqueLinks[resolved.String()] = struct{}{}
			}
		}

		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}

	walk(document)

	links := make([]string, 0, len(uniqueLinks))
	for link := range uniqueLinks {
		links = append(links, link)
	}
	return result, links
}

func detectHTMLVersion(doctype *html.Node) string {
	declaration := strings.ToLower(doctype.Data)
	for _, attr := range doctype.Attr {
		declaration += " " + strings.ToLower(attr.Val)
	}

	switch {
	case strings.Contains(declaration, "xhtml 1.1"):
		return "XHTML 1.1"
	case strings.Contains(declaration, "xhtml 1.0"):
		return "XHTML 1.0"
	case strings.Contains(declaration, "html 4.01"):
		return "HTML 4.01"
	case strings.Contains(declaration, "html 3.2"):
		return "HTML 3.2"
	case strings.TrimSpace(declaration) == "html":
		return "HTML5"
	default:
		return "Unknown doctype"
	}
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func attribute(node *html.Node, name string) (string, bool) {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val, true
		}
	}
	return "", false
}

func containsPasswordInput(form *html.Node) bool {
	var walk func(*html.Node) bool
	walk = func(node *html.Node) bool {
		if node.Type == html.ElementNode && node.Data == "input" {
			inputType, _ := attribute(node, "type")
			if strings.EqualFold(inputType, "password") {
				return true
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if walk(child) {
				return true
			}
		}
		return false
	}
	return walk(form)
}

func resolveWebLink(baseURL *url.URL, href string) (*url.URL, bool) {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return nil, false
	}

	parsedLink, err := url.Parse(href)
	if err != nil {
		return nil, false
	}
	resolved := baseURL.ResolveReference(parsedLink)
	if resolved.Scheme != "http" && resolved.Scheme != "https" {
		return nil, false
	}
	resolved.Fragment = ""
	return resolved, true
}

func (a *Analyzer) countInaccessibleLinks(ctx context.Context, links []string) (int, error) {
	if len(links) == 0 {
		return 0, nil
	}

	workerCount := a.workers
	if workerCount > len(links) {
		workerCount = len(links)
	}

	jobs := make(chan string)
	results := make(chan bool)
	var workers sync.WaitGroup

	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for link := range jobs {
				results <- a.linkIsAccessible(ctx, link)
			}
		}()
	}

	go func() {
		defer close(jobs)
		for _, link := range links {
			select {
			case jobs <- link:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		workers.Wait()
		close(results)
	}()

	inaccessible := 0
	for accessible := range results {
		if !accessible {
			inaccessible++
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return inaccessible, nil
}

func (a *Analyzer) linkIsAccessible(ctx context.Context, link string) bool {
	statusCode, err := a.requestLink(ctx, http.MethodHead, link)
	if err != nil {
		return false
	}
	if statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented {
		statusCode, err = a.requestLink(ctx, http.MethodGet, link)
		if err != nil {
			return false
		}
	}
	return statusCode < http.StatusBadRequest
}

func (a *Analyzer) requestLink(ctx context.Context, method, link string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, link, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", "WebAnalyzer/1.0")

	response, err := a.client.Do(request)
	if err != nil {
		return 0, err
	}
	response.Body.Close()
	return response.StatusCode, nil
}
