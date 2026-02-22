.PHONY: build run dev test check fmt vet gen-streams demo clean all

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

build:
	go build -ldflags="-X main.version=$(VERSION)" -o bin/prism ./cmd/prism

run: build
	./bin/prism

dev:
	cd web && npm run dev

test:
	go test -v -race ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

check: fmt vet
	gofmt -s -l . | (! grep .) || (echo "gofmt check failed" && exit 1)
	go mod tidy
	staticcheck ./...
	go test -race ./...
	cd web && npx tsc --noEmit

gen-streams:
	go run ./test/tools/gen-streams/

demo: build gen-streams
	@echo "Starting prism + all 9 test streams..."
	@./bin/prism & PRISM_PID=$$!; \
	trap "kill $$PRISM_PID 2>/dev/null" EXIT; \
	sleep 2; \
	go run ./test/tools/srt-push/ --all; \
	wait $$PRISM_PID

clean:
	rm -rf bin/
	rm -rf test/streams/
	rm -rf test/sources/

all: check build
	cd web && npm install && npm run build
