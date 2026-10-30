package linkchecker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func response(request *http.Request, status int) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    request,
	}
}

func TestPoolChecks450LinksWithBoundedConcurrency(t *testing.T) {
	const expectedWorkers = 10
	links := make([]string, 450)
	for index := range links {
		links[index] = fmt.Sprintf("https://example.com/page/%d", index)
	}

	var active atomic.Int32
	var maximum atomic.Int32
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
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
		if strings.HasSuffix(request.URL.Path, "/100") {
			status = http.StatusNotFound
		}
		return response(request, status), nil
	})

	pool := New(&http.Client{Transport: transport}, 20)
	progressCalls := 0
	lastCompleted := 0
	lastTotal := 0
	results, err := pool.Check(context.Background(), links, func(completed, total int) {
		progressCalls++
		lastCompleted = completed
		lastTotal = total
	})
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(results) != len(links) {
		t.Fatalf("result count = %d, want %d", len(results), len(links))
	}
	if progressCalls != len(links)+1 || lastCompleted != len(links) || lastTotal != len(links) {
		t.Errorf("progress calls=%d final=%d/%d, want %d calls and %d/%d", progressCalls, lastCompleted, lastTotal, len(links)+1, len(links), len(links))
	}
	if maximum.Load() > expectedWorkers {
		t.Errorf("maximum concurrency = %d, want at most %d", maximum.Load(), expectedWorkers)
	}
	if maximum.Load() < 2 {
		t.Errorf("maximum concurrency = %d, expected concurrent checks", maximum.Load())
	}

	inaccessible := 0
	for _, result := range results {
		if !result.Accessible() {
			inaccessible++
		}
	}
	if inaccessible != 1 {
		t.Errorf("inaccessible count = %d, want 1", inaccessible)
	}
}

func TestWorkerCountScalesWithUniqueLinks(t *testing.T) {
	pool := New(&http.Client{}, 20)
	tests := []struct {
		links int
		want  int
	}{
		{links: 1, want: 1},
		{links: 100, want: 5},
		{links: 101, want: 10},
		{links: 500, want: 10},
		{links: 501, want: 15},
		{links: 1500, want: 15},
		{links: 1501, want: 20},
		{links: 2524, want: 20},
	}

	for _, test := range tests {
		if got := pool.workerCount(test.links); got != test.want {
			t.Errorf("workerCount(%d) = %d, want %d", test.links, got, test.want)
		}
	}
}

func TestPoolFallsBackToGet(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		mu.Lock()
		methods = append(methods, request.Method)
		mu.Unlock()
		if request.Method == http.MethodHead {
			return response(request, http.StatusMethodNotAllowed), nil
		}
		return response(request, http.StatusOK), nil
	})

	results, err := New(&http.Client{Transport: transport}, 1).
		Check(context.Background(), []string{"https://example.com/page"}, nil)
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if len(results) != 1 || !results[0].Accessible() {
		t.Fatalf("results = %+v, want one accessible result", results)
	}
	if strings.Join(methods, ",") != "HEAD,GET" {
		t.Errorf("methods = %v, want HEAD then GET", methods)
	}
}

func TestPoolHonorsCancellation(t *testing.T) {
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := New(&http.Client{Transport: transport}, 2).
		Check(ctx, []string{"https://one.test", "https://two.test"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestPoolReturnsCompletedResultsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if strings.HasSuffix(request.URL.Path, "/one") {
			return response(request, http.StatusOK), nil
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	links := []string{
		"https://example.com/one",
		"https://example.com/two",
		"https://example.com/three",
	}

	results, err := New(&http.Client{Transport: transport}, 1).
		Check(ctx, links, func(completed, _ int) {
			if completed == 1 {
				cancel()
			}
		})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(results) == 0 || len(results) >= len(links) {
		t.Fatalf("result count = %d, want some but not all results", len(results))
	}
}
