# Contributing to Prism

Thank you for your interest in contributing to Prism.

## Getting Started

1. Fork the repository
2. Create a feature branch from `main`
3. Make your changes
4. Ensure `make check` passes
5. Submit a pull request

## Development Requirements

- Go 1.24+
- Node.js 22+ (for the web viewer)

## Code Quality

All changes must pass `make check`, which runs:

- `gofmt -s` — Go code formatting with simplification
- `go vet` — Go static analysis
- `go test -race` — All tests with the race detector enabled
- `npx tsc --noEmit` — TypeScript type checking (strict mode)

### Go Conventions

- Format with `gofmt -s`
- All tests must pass with `-race`
- Use `log/slog` for structured logging
- Prefer standard library over third-party dependencies
- Write table-driven tests where appropriate
- Add fuzz tests for any code that parses untrusted input
- Use `t.Parallel()` in tests

### TypeScript Conventions

- Strict mode (`noUnusedLocals`, `noUnusedParameters`)
- Prefix intentionally unused parameters with `_`
- No runtime dependencies — vanilla TypeScript only

## Running Tests

```bash
# Run all tests with race detector
make test

# Run tests for a specific package
go test -v -race ./demux/

# Run fuzz tests (default 10s, adjust as needed)
go test -fuzz=FuzzParseAnnexB -fuzztime=30s ./demux/

# Run benchmarks
go test -bench=. -benchmem ./distribution/
```

## Project Structure

See the [Architecture section](README.md#architecture) in the README for an overview of the codebase.

## Pull Requests

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if behavior changes
- Ensure `make check` passes before submitting

## License

By contributing to Prism, you agree that your contributions will be licensed under the [MIT License](LICENSE).
