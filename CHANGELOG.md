# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-04-26

### Added
- `Qdrant` plugin entry point implementing the Genkit `api.Plugin` interface
- `Indexer` and `Retriever` for Qdrant collections via gRPC
- Support for both single-vector and named-vector collection layouts
- Filter passthrough via `RetrieverOptions.Filter` (accepts `map[string]any` JSON-shaped filter or `*qdrant.Filter` directly)
  - Supported map operators: `must`, `should`, `must_not` with `match.value`, `match.any`, `range.{gt,gte,lt,lte}`
  - Geo / datetime / nested filters via direct `*qdrant.Filter` pass-through
- `RetrieverOptions.ScoreThreshold` for minimum similarity cutoff
- Idempotent point IDs derived from MD5 of document content (formatted as UUID)
- Per-endpoint client deduplication when multiple `Config`s share connection params
- Topic-specific documentation under `docs/` (getting-started, configuration, named-vectors, filtering, API reference, roadmap)
- Three runnable examples: `examples/{single-vector,named-vectors,with-filter}`
- Unit tests for `internal/convert` (~78% coverage) and the plugin's config/dispatch logic
- Integration test using `testcontainers-go` to spin up real Qdrant (skipped under `-short`)

### Notes
- Genkit Go 1.x does not yet expose a first-class `ai.Indexer` interface; this plugin defines a minimal `IndexerHandle` interface that will align to Genkit's once available.
- The plugin uses Qdrant's gRPC API exclusively (port `6334` by default). The official Qdrant Go client is gRPC-only; an optional REST transport is on the roadmap.

### Compatibility
- Genkit Go: `1.0.0`
- Qdrant: `1.10+`
- Go: `1.25+`

[Unreleased]: https://github.com/arnstarn/qdrant-genkit-go/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/arnstarn/qdrant-genkit-go/releases/tag/v0.1.0
