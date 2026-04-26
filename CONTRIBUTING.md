# Contributing

Contributions welcome.

## Development setup

```bash
git clone https://github.com/arnstarn/qdrant-genkit-go.git
cd qdrant-genkit-go
go mod download
```

## Running tests

Tests use [testcontainers-go](https://golang.testcontainers.org/) to spin up a real Qdrant instance. Requires Docker.

```bash
go test ./...
```

## Filing issues

When reporting bugs, please include:
- Go version (`go version`)
- Qdrant version (`curl http://your-qdrant:6333/`)
- Genkit Go version (from `go.mod`)
- Minimal reproducer code

## Submitting changes

1. Fork the repo
2. Create a topic branch
3. Add tests for new behavior
4. `go fmt ./...` and `go vet ./...` should pass
5. Open a PR with a clear description

## API stability

This plugin is pre-1.0. API may change. Once we tag v1.0.0, we'll follow [SemVer](https://semver.org).

## Code of conduct

Be respectful. Assume good faith.
