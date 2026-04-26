# Roadmap

This plugin is pre-1.0. The items below are planned but not yet implemented. Track progress on the [issue tracker](https://github.com/arnstarn/qdrant-genkit-go/issues), and PRs are welcome — see [CONTRIBUTING.md](../CONTRIBUTING.md).

## Near-term

### Optional REST transport

The plugin currently uses Qdrant's gRPC API exclusively (the official Go client is gRPC-only). Adding an optional HTTP REST transport — selectable per `Config` — would help users behind environments where gRPC is awkward (some corporate proxies, browser-side use cases via WASM).

### Sparse vectors / hybrid search

Qdrant supports sparse vectors (BM25-style lexical signals) alongside dense vectors. Hybrid search fuses both for results that respect both semantic similarity and exact-keyword presence. Planned as an optional named slot pattern.

### Recommend / Discovery API support

Beyond pure semantic search, Qdrant exposes "recommend" (find points similar to a positive example, dissimilar to negatives) and "discovery" (steer search using context examples). These will be exposed as additional retriever modes.

### Snapshot helpers

Convenience wrappers around Qdrant's snapshot API for backup and migration workflows.

### mTLS auth

Mutual TLS for self-hosted deployments where API keys aren't sufficient.

## Considered, not committed

- Cluster-aware client (sharded collections, leader hints)
- Quantization configuration helpers
- Native multi-tenancy via payload-based partitioning patterns
- Query-time payload selectors (exclude large fields from results)

## Out of scope

- Anything that re-implements an embedder. This plugin is transport between Genkit's embedder abstraction and Qdrant — embedders are wired via separate Genkit plugins.
- Arbitrary vector DB abstraction. Qdrant-specific features are intentionally exposed; this is not a generic interface.

## Suggesting changes

Open an issue describing the use case. PRs that add features should include tests (the test suite uses [testcontainers-go](https://golang.testcontainers.org/) — see CONTRIBUTING.md).
