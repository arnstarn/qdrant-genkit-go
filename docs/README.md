# Documentation

Detailed documentation for `qdrant-genkit-go`, the Qdrant plugin for [Firebase Genkit](https://genkit.dev) Go.

For a quick overview, install instructions, and a minimal example, see the [top-level README](../README.md).

## Guides

- [Getting started](getting-started.md) — install, run Qdrant locally, configure an embedder, index and retrieve documents, troubleshoot common errors.
- [Configuration](configuration.md) — every `Config` and `ClientParams` field, defaults, and when to override them.
- [Connection patterns](connection.md) — gRPC vs REST, self-hosted, Qdrant Cloud, TLS, common connection errors.
- [Named vectors](named-vectors.md) — deep dive on Qdrant's multi-vector pattern with a full multi-modal example.
- [Filtering](filtering.md) — Qdrant filter syntax with practical examples (`must`, `should`, `must_not`, range, geo, full-text).
- [API reference](api-reference.md) — godoc-style reference of public types and functions.
- [Roadmap](roadmap.md) — what's planned (optional REST transport, sparse vectors, hybrid search, etc.).

## Contributing

See [CONTRIBUTING.md](../CONTRIBUTING.md) for development setup, test instructions, and PR guidelines.

## Examples

Working code in the [`examples/`](../examples/) directory:

- `examples/single-vector/` — basic single-vector collection
- `examples/named-vectors/` — multi-modal collection with separate text/image vector slots
- `examples/with-filter/` — retrieval with metadata filtering
