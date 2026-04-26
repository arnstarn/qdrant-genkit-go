# Getting started

This walkthrough takes you from zero to a running Genkit Go program that indexes documents into Qdrant and retrieves them by semantic search.

## Prerequisites

- Go 1.23 or newer (`go version`)
- Docker, for running Qdrant locally (or a Qdrant Cloud cluster)
- A Genkit-compatible embedder. Any of the following works:
  - Vertex AI (`github.com/firebase/genkit/go/plugins/vertexai`)
  - Google AI / Gemini (`github.com/firebase/genkit/go/plugins/googleai`)
  - An OpenAI-compatible embedding endpoint (any community plugin that implements `ai.Embedder`)

## Step 1 — Install the plugin

```bash
go get github.com/arnstarn/qdrant-genkit-go
```

Add it to your `go.mod` along with `github.com/firebase/genkit/go`.

## Step 2 — Run Qdrant locally

The fastest path is the official Docker image:

```bash
docker run -d --name qdrant \
  -p 6333:6333 -p 6334:6334 \
  -v qdrant-storage:/qdrant/storage \
  qdrant/qdrant:latest
```

Verify it's reachable:

```bash
curl http://localhost:6333/
# → {"title":"qdrant - vector search engine","version":"..."}
```

For production use, see the [Qdrant deployment guide](https://qdrant.tech/documentation/guides/installation/) or sign up for [Qdrant Cloud](https://cloud.qdrant.io/).

## Step 3 — Create a collection

The plugin does not create collections for you. You must create one with the right vector size for your embedder.

For a single-vector collection sized for a 768-dimensional embedder:

```bash
curl -X PUT http://localhost:6333/collections/my_collection \
  -H "Content-Type: application/json" \
  -d '{
    "vectors": {
      "size": 768,
      "distance": "Cosine"
    }
  }'
```

Match the `size` to whatever your embedder produces. For multi-vector collections, see [named-vectors.md](named-vectors.md).

## Step 4 — Configure the plugin

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

    // Wire up your Genkit embedder of choice. The Qdrant plugin doesn't care
    // which embedder you use — it stores whatever vectors the embedder returns.
    var embedder ai.Embedder // returned by your embedding plugin

    g := genkit.Init(ctx,
        genkit.WithPlugins(&qdrantplugin.Qdrant{
            Configs: []qdrantplugin.Config{{
                CollectionName: "my_collection",
                ClientParams: qdrantplugin.ClientParams{
                    Host: "localhost",
                    Port: 6334, // gRPC; REST is on 6333
                },
                Embedder: embedder,
            }},
        }),
    )
    _ = g
}
```

## Step 5 — Index documents

```go
indexer := qdrantplugin.Indexer(g, "my_collection")

docs := []*ai.Document{
    ai.DocumentFromText("Cosine similarity measures the angle between two vectors.", nil),
    ai.DocumentFromText("Qdrant supports filtering by payload fields at query time.", nil),
}

if err := indexer.Index(ctx, docs); err != nil {
    log.Fatal(err)
}
```

## Step 6 — Retrieve documents

```go
retriever := qdrantplugin.Retriever(g, "my_collection")

resp, err := retriever.Retrieve(ctx, &ai.RetrieverRequest{
    Query:   ai.DocumentFromText("how does similarity search work?", nil),
    Options: &qdrantplugin.RetrieverOptions{K: 5},
})
if err != nil {
    log.Fatal(err)
}

for _, doc := range resp.Documents {
    log.Println(doc.Content[0].Text)
}
```

## Common errors

### `connection refused` on port 6334

The plugin connects via gRPC on `6334` by default. Qdrant isn't running, or `Host`/`Port` don't match. Check `docker ps` and `curl http://localhost:6333/` (REST port — quick reachability check).

### `Wrong input: Vector dimension error: expected dim X, got Y`

The collection's vector size doesn't match what your embedder produces. Re-create the collection with the correct `size`, or switch embedders.

### `Not found: Collection my_collection doesn't exist`

You need to create the collection before initializing the plugin. See Step 3.

### `Unauthorized` when using Qdrant Cloud

You're missing the API key. Set `ClientParams.APIKey` and `ClientParams.UseTLS = true`.

### Empty `resp.Documents`

Either the collection is empty, your filter is too strict, or your embedder is returning vectors that don't match what was indexed (e.g., you indexed with one model and queried with another). Re-index with a consistent embedder.

## Next steps

- [Configuration reference](configuration.md) — every option explained
- [Named vectors](named-vectors.md) — multi-modal and multi-embedder setups
- [Filtering](filtering.md) — narrow results with payload predicates
