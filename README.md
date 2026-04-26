# qdrant-genkit-go

Qdrant plugin for [Firebase Genkit](https://genkit.dev) Go. Provides `Indexer` and `Retriever` implementations backed by [Qdrant](https://qdrant.tech) — a high-performance vector database written in Rust.

> Status: pre-release. API may change before v1.0.

## Features

- **Indexer**: upsert documents into Qdrant collections (text + metadata + vector)
- **Retriever**: semantic search by vector with optional metadata filtering
- **Single-vector and named-vector collections** — supports both Qdrant collection layouts
- **Cloud or self-hosted Qdrant** — configurable endpoint, API key, TLS
- **Mirrors the JS/TS [`@genkit-ai/qdrant`](https://github.com/qdrant/qdrant-genkit) plugin's API** for cross-language familiarity

## Install

```bash
go get github.com/arnstarn/qdrant-genkit-go
```

## Quick start (single-vector collection)

```go
package main

import (
    "context"
    "log"

    "github.com/firebase/genkit/go/ai"
    "github.com/firebase/genkit/go/genkit"
    qdrantplugin "github.com/arnstarn/qdrant-genkit-go"
)

func main() {
    ctx := context.Background()

    // Set up your Genkit instance with an embedder of your choice (e.g., Vertex AI,
    // OpenAI, or any OpenAI-compatible service). The Qdrant plugin doesn't care
    // which embedder you use — it just stores the vectors the embedder produces.
    var embedder ai.Embedder // wire up via your embedding plugin

    g, err := genkit.Init(ctx,
        genkit.WithPlugins(&qdrantplugin.Qdrant{
            Configs: []qdrantplugin.Config{{
                CollectionName:     "my_collection",
                ClientParams:       qdrantplugin.ClientParams{
                    Host:   "localhost",
                    Port:   6333,
                    APIKey: "your-api-key",
                },
                Embedder:           embedder,
                ContentPayloadKey:  "content",   // optional, default "content"
                MetadataPayloadKey: "metadata",  // optional, default "metadata"
            }},
        }),
    )
    if err != nil {
        log.Fatal(err)
    }

    // Retrieve documents semantically
    retriever := qdrantplugin.Retriever(g, "my_collection")
    resp, err := ai.Retrieve(ctx, retriever,
        ai.WithRetrieverOpts(&ai.RetrieverOptions{K: 5}),
        ai.WithRetrieverText("how do I configure logging?"),
    )
    if err != nil {
        log.Fatal(err)
    }
    for _, doc := range resp.Documents {
        log.Println(doc.Content[0].Text)
    }
}
```

## Named-vector collections

Qdrant supports collections with multiple named vectors (different dimensions per vector slot). Specify which vector slot to use:

```go
&qdrantplugin.Qdrant{
    Configs: []qdrantplugin.Config{{
        CollectionName: "multi_modal",
        ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6333},
        Embedder:       textEmbedder,
        VectorName:     "text", // name of the vector slot in this collection
    }, {
        CollectionName: "multi_modal",
        ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6333},
        Embedder:       imageEmbedder,
        VectorName:     "image",
    }},
}
```

You'll get one retriever per `(collection, vector)` pair. See `examples/named-vectors/`.

## Filtering at retrieve time

Pass a [Qdrant filter](https://qdrant.tech/documentation/concepts/filtering/) to scope results:

```go
ai.Retrieve(ctx, retriever,
    ai.WithRetrieverOpts(&ai.RetrieverOptions{
        K: 5,
        Filter: map[string]any{
            "must": []map[string]any{
                {"key": "metadata.lang", "match": map[string]any{"value": "go"}},
            },
        },
    }),
    ai.WithRetrieverText("query"),
)
```

See `examples/with-filter/`.

## Configuration reference

| Field | Type | Default | Description |
|---|---|---|---|
| `CollectionName` | string | required | Qdrant collection name |
| `ClientParams.Host` | string | `localhost` | Qdrant host |
| `ClientParams.Port` | int | `6333` | HTTP REST port |
| `ClientParams.APIKey` | string | `""` | API key for authentication (optional) |
| `ClientParams.UseTLS` | bool | `false` | Connect over HTTPS |
| `Embedder` | `ai.Embedder` | required | Genkit embedder used for both indexing and retrieval queries |
| `ContentPayloadKey` | string | `content` | Payload key holding document text |
| `MetadataPayloadKey` | string | `metadata` | Payload key holding metadata |
| `VectorName` | string | `""` (single-vector) | Name of vector slot for named-vector collections |

## Compatibility

- **Genkit Go**: 1.0+
- **Qdrant**: 1.10+ (uses HTTP REST API)
- **Go**: 1.23+

## Roadmap

- [ ] gRPC transport (currently HTTP REST only)
- [ ] Sparse vectors / hybrid search (BM25-style)
- [ ] Recommend / Discovery API support
- [ ] Snapshot helpers
- [ ] mTLS auth

PRs welcome — see `CONTRIBUTING.md`.

## Examples

- `examples/single-vector/` — basic usage with a single-vector collection
- `examples/named-vectors/` — multi-modal collection with separate text/image vector slots
- `examples/with-filter/` — retrieval with metadata filtering

## License

Apache-2.0. See `LICENSE`.

## Acknowledgments

- API shape mirrors the [JS/TS Qdrant Genkit plugin](https://github.com/qdrant/qdrant-genkit) for cross-language familiarity
- Built on top of the official [Qdrant Go client](https://github.com/qdrant/go-client)

This is an unofficial plugin and is not affiliated with Qdrant or Google.
