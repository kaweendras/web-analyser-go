package linkchecker

import (
	"context"
	"net/http"
	"sync"
)

// Result describes the outcome of checking one URL.
type Result struct {
	URL        string
	StatusCode int
	Err        error
}

// Accessible reports whether the URL returned a non-error HTTP status.
func (r Result) Accessible() bool {
	return r.Err == nil && r.StatusCode < http.StatusBadRequest
}

// Pool checks links with a fixed number of workers.
type Pool struct {
	client     *http.Client
	maxWorkers int
}

// New creates a link-checking worker pool.
func New(client *http.Client, maxWorkers int) *Pool {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	return &Pool{client: client, maxWorkers: maxWorkers}
}

// ProgressFunc receives completed and total link counts.
type ProgressFunc func(completed, total int)

// Check every supplied URL once and returns one result per URL.
func (p *Pool) Check(ctx context.Context, links []string, progress ProgressFunc) ([]Result, error) {
	if len(links) == 0 {
		if progress != nil {
			progress(0, 0)
		}
		return nil, nil
	}

	workerCount := p.workerCount(len(links))
	if progress != nil {
		progress(0, len(links))
	}

	jobs := make(chan string)
	results := make(chan Result, len(links))
	var workers sync.WaitGroup

	for range workerCount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for link := range jobs {
				results <- p.checkOne(ctx, link)
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

	checked := make([]Result, 0, len(links))
	for result := range results {
		checked = append(checked, result)
		if progress != nil {
			progress(len(checked), len(links))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return checked, nil
}

func (p *Pool) workerCount(linkCount int) int {
	target := 5
	switch {
	case linkCount > 1500:
		target = 20
	case linkCount > 500:
		target = 15
	case linkCount > 100:
		target = 10
	}

	if target > p.maxWorkers {
		target = p.maxWorkers
	}
	if target > linkCount {
		target = linkCount
	}
	return target
}

func (p *Pool) checkOne(ctx context.Context, link string) Result {
	statusCode, err := p.request(ctx, http.MethodHead, link)
	if err == nil && (statusCode == http.StatusMethodNotAllowed || statusCode == http.StatusNotImplemented) {
		statusCode, err = p.request(ctx, http.MethodGet, link)
	}
	return Result{URL: link, StatusCode: statusCode, Err: err}
}

func (p *Pool) request(ctx context.Context, method, link string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, link, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", "WebAnalyzer/1.0")
	if method == http.MethodGet {
		request.Header.Set("Range", "bytes=0-0")
	}

	response, err := p.client.Do(request)
	if err != nil {
		return 0, err
	}
	response.Body.Close()
	return response.StatusCode, nil
}
