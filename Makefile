.PHONY: build run dev test check fmt vet web-install web-build gen-streams demo demo-full kill-prism clean all

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
	go test -race -cover ./...
	govulncheck ./...
	cd web && npx tsc --noEmit

web-install:
	@cd web && npm install

web-build: web-install
	@cd web && npm run build

gen-streams:
	go run ./test/tools/gen-streams/

# Kill any existing prism processes and free the required ports
kill-prism:
	@lsof -ti :6000 2>/dev/null | xargs kill 2>/dev/null || true
	@lsof -ti :4443 2>/dev/null | xargs kill 2>/dev/null || true
	@lsof -ti :4444 2>/dev/null | xargs kill 2>/dev/null || true
	@sleep 1

demo: build web-build kill-prism
	@echo "Starting Prism with bundled demo stream..."
	@echo "(Accept the self-signed certificate warning in your browser)"
	@./bin/prism & PRISM_PID=$$!; \
	trap "kill $$PRISM_PID 2>/dev/null; wait $$PRISM_PID 2>/dev/null" EXIT INT TERM; \
	sleep 2; \
	open "https://localhost:4444/?stream=demo" 2>/dev/null || xdg-open "https://localhost:4444/?stream=demo" 2>/dev/null || echo "Open https://localhost:4444/?stream=demo in your browser"; \
	go run ./test/tools/srt-push/ --file test/harness/BigBuckBunny_256x144-24fps.ts --key live/demo --duration 28.8; \
	wait $$PRISM_PID

demo-full: build web-build kill-prism gen-streams
	@echo "Starting Prism with all 9 broadcast-realistic test streams..."
	@echo "(Accept the self-signed certificate warning in your browser)"
	@./bin/prism & PRISM_PID=$$!; \
	trap "kill $$PRISM_PID 2>/dev/null; wait $$PRISM_PID 2>/dev/null" EXIT INT TERM; \
	sleep 2; \
	open "https://localhost:4444/" 2>/dev/null || xdg-open "https://localhost:4444/" 2>/dev/null || echo "Open https://localhost:4444/ in your browser"; \
	go run ./test/tools/srt-push/ --all; \
	wait $$PRISM_PID

clean:
	rm -rf bin/
	rm -rf test/streams/
	rm -rf test/sources/

all: check build web-build
