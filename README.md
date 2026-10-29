# Web Page Analyzer

A small Go web application that accepts a URL and reports useful information about the server-rendered HTML page. Link accessibility checks run concurrently through a bounded worker pool, while the browser displays live progress.

## Features

- Detects the HTML version and page title.
- Counts H1 through H6 headings.
- Counts internal and external link occurrences.
- Checks every unique HTTP/HTTPS link for accessibility.
- Detects likely login forms by looking for password inputs.
- Streams link-checking progress to a responsive web interface.
- Handles redirects, network failures, invalid URLs, and analysis timeouts.

## Project structure

```text
cmd/web-analyser/          Application entry point and server configuration
internal/analyzer/         Page fetching, HTML parsing, and analysis rules
internal/linkchecker/      Bounded concurrent link-checking worker pool
internal/web/              HTTP handlers and embedded HTML interface
Dockerfile                 Multi-stage container build
Makefile                   Common development commands
```

## Run locally

### Requirements

- Go 1.27 or newer

### Steps

```bash
git clone https://github.com/kaweendras/web-analyser-go.git
cd web-analyser-go
go mod download
go run ./cmd/web-analyser
```

Open [http://localhost:8080](http://localhost:8080), enter a complete HTTP or HTTPS URL, and select **Analyze page**.

To create a local binary:

```bash
go build ./cmd/web-analyser
```

On Windows PowerShell, the output executable will be `web-analyser.exe`.

## Run with Docker

### Requirements

- Docker Desktop or Docker Engine

### Steps

```bash
docker build -t web-analyser .
docker run --rm -p 8080:8080 web-analyser
```

Then open [http://localhost:8080](http://localhost:8080). The container runs as a non-root user and includes the CA certificates required for HTTPS checks.

## Make commands

If GNU Make is installed:

```bash
make run           # Start the application
make build         # Build the local executable
make fmt           # Format Go source files
make vet           # Run static checks
make test          # Run unit tests
make coverage      # Show package test coverage
make check         # Format, vet, and test
make docker-build  # Build the Docker image
make docker-run    # Run the Docker image on port 8080
```

## Tests

```bash
go test ./...
go vet ./...
go test -cover ./...
```

Tests use local `httptest` servers and fake dependencies rather than making requests to public websites.

## Architecture and decisions

The application uses a small layered structure:

1. The web handler validates the request flow and streams newline-delimited JSON progress events.
2. The analyzer downloads and parses the submitted page with `golang.org/x/net/html`.
3. The link checker distributes unique URLs to a bounded worker pool.
4. The embedded HTML template renders results without requiring a separate frontend build.

The worker count scales with the number of unique links and is capped at 20. This improves large-page performance without creating an unbounded number of goroutines or overwhelming remote servers.

Network requests have an 8-second client timeout, and a complete analysis has a 2-minute deadline. Structured application messages use the standard library's `log/slog` package.

## Assumptions

- A link is internal when its hostname matches the analyzed page's hostname.
- Internal and external totals count link occurrences on the submitted page.
- Link accessibility checks are deduplicated, so each unique destination is checked once.
- Only HTTP and HTTPS links are analyzed; fragments, email links, and other schemes are ignored.
- A form containing an `<input type="password">` is treated as a likely login form.
- The analyzer inspects server-returned HTML and does not execute client-side JavaScript.
- A link is inaccessible when the request fails or returns HTTP status 400 or higher.
- `HEAD` is attempted first; `GET` with a small range is used when a server rejects `HEAD`.

## Possible improvements

- Display the individual inaccessible URLs and their status codes.
- Allow timeout and worker limits to be configured through environment variables.
- Add per-host rate limiting and retry rules for temporary failures.
- Cache recent results to avoid repeating identical checks.
- Add end-to-end browser tests and automated CI checks.
- Support JavaScript-rendered pages through an optional browser-based analyzer.
