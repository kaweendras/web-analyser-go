APP_NAME := web-analyser

.PHONY: run build fmt vet test coverage check docker-build docker-run

run:
	go run ./cmd/web-analyser

build:
	go build ./cmd/web-analyser

fmt:
	gofmt -w cmd internal

vet:
	go vet ./...

test:
	go test ./...

coverage:
	go test -cover ./...

check: fmt vet test

docker-build:
	docker build -t $(APP_NAME) .

docker-run:
	docker run --rm -p 8080:8080 $(APP_NAME)
