# qdrant-genkit-go

Qdrant plugin for [Firebase Genkit](https://genkit.dev) Go. Provides `Indexer` and `Retriever` implementations backed by [Qdrant](https://qdrant.tech).

> **Status**: pre-release. API may change before v1.0.

## Features

- Indexer and Retriever for Qdrant collections
- Single-vector and named-vector collection layouts
- Cloud or self-hosted Qdrant (configurable host, port, API key, TLS)
- Metadata filtering at retrieve time
- API mirrors the JS/TS [`@genkit-ai/qdrant`](https://github.com/qdrant/qdrant-genkit) plugin for cross-language familiarity

## Install

```bash
go get github.com/arnstarn/qdrant-genkit-go
```

## Minimal example

```go
import (
    "github.com/firebase/genkit/go/ai"
    "github.com/firebase/genkit/go/genkit"
    qdrantplugin "github.com/arnstarn/qdrant-genkit-go"
)

g, err := genkit.Init(ctx,
    genkit.WithPlugins(&qdrantplugin.Qdrant{
        Configs: []qdrantplugin.Config{{
            CollectionName: "my_collection",
            ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6334},
            Embedder:       embedder, // any ai.Embedder
        }},
    }),
)

retriever := qdrantplugin.Retriever(g, "my_collection")
resp, _ := retriever.Retrieve(ctx, &ai.RetrieverRequest{
    Query:   ai.DocumentFromText("how does similarity search work?", nil),
    Options: &qdrantplugin.RetrieverOptions{K: 5},
})
```

## Documentation

- [Getting started](docs/getting-started.md) — install, run Qdrant, configure an embedder, index and retrieve
- [Configuration](docs/configuration.md) — every option explained
- [Connection patterns](docs/connection.md) — gRPC, TLS, Qdrant Cloud, self-hosted
- [Named vectors](docs/named-vectors.md) — multi-vector collections
- [Filtering](docs/filtering.md) — Qdrant filter syntax with examples
- [API reference](docs/api-reference.md) — godoc-style overview
- [Roadmap](docs/roadmap.md) — what's planned
- [Changelog](CHANGELOG.md) — release history

See [`docs/README.md`](docs/README.md) for the docs index.

## Compatibility

- Genkit Go 1.0+
- Qdrant 1.10+
- Go 1.23+

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache-2.0. See [LICENSE](LICENSE).

## Acknowledgments

- API shape mirrors the [JS/TS Qdrant Genkit plugin](https://github.com/qdrant/qdrant-genkit)
- Built on top of the official [Qdrant Go client](https://github.com/qdrant/go-client)

This is an unofficial plugin and is not affiliated with Qdrant or Google.
