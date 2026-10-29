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

## Architecture diagram

```mermaid
flowchart LR
    Browser[Web browser] --> Handler[HTTP handler]
    Handler --> Analyzer[Page analyzer]
    Analyzer --> Page[Target web page]
    Analyzer --> Pool[Link checker worker pool]
    Pool --> Links[Unique page links]
    Handler -. Progress events .-> Browser
    Analyzer --> Handler
```
