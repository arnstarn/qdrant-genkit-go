# Named vectors

Qdrant collections support multiple **named vectors** per point. Each named vector is independently sized and independently distance-typed, but they share the same point ID and payload. This is the right shape when you want one logical record (a document, an asset, a product) to be searchable by several embeddings — for example, a text description and an image, or a code snippet and a natural-language summary.

This plugin treats every `(collection, vector_name)` pair as its own indexer + retriever, while sharing the underlying point storage.

## When to use named vectors

Reach for named vectors when **any** of the following apply:

- You want to retrieve the same logical records by different modalities (text, image, audio).
- You want to keep two embedding models in play (e.g., a small fast one for first-pass retrieval and a larger one for reranking).
- You're storing a code chunk that you want to find both via natural-language queries (text embedder) and via code-similarity queries (code embedder).

If you only ever embed one way, stick with a single-vector collection — it's simpler and uses less storage.

## Creating the collection in Qdrant

The plugin does not create collections. Provision the collection with two named vector slots before initializing the plugin. For a `text` slot at 768d and an `image` slot at 512d:

```bash
curl -X PUT http://localhost:6333/collections/multi_modal \
  -H "Content-Type: application/json" \
  -d '{
    "vectors": {
      "text":  { "size": 768, "distance": "Cosine" },
      "image": { "size": 512, "distance": "Cosine" }
    }
  }'
```

Distance functions and sizes can differ per slot. Match each `size` to the embedder you'll wire up.

## Wiring up the plugin

Declare one `Config` per slot. They share `CollectionName` and `ClientParams`; they differ in `VectorName` and `Embedder`.

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

    var (
        textEmbedder  ai.Embedder // 768-d text embedder
        imageEmbedder ai.Embedder // 512-d image embedder
    )

    g := genkit.Init(ctx,
        genkit.WithPlugins(&qdrantplugin.Qdrant{
            Configs: []qdrantplugin.Config{
                {
                    CollectionName: "multi_modal",
                    ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6334},
                    Embedder:       textEmbedder,
                    VectorName:     "text",
                },
                {
                    CollectionName: "multi_modal",
                    ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6334},
                    Embedder:       imageEmbedder,
                    VectorName:     "image",
                },
            },
        }),
    )

    // Retrieve the per-slot retrievers.
    textRetriever  := qdrantplugin.Retriever(g, "multi_modal/text")
    imageRetriever := qdrantplugin.Retriever(g, "multi_modal/image")

    // Search by text.
    textResp, err := textRetriever.Retrieve(ctx, &ai.RetrieverRequest{
        Query:   ai.DocumentFromText("blue running shoes", nil),
        Options: &qdrantplugin.RetrieverOptions{K: 5},
    })
    if err != nil {
        log.Fatal(err)
    }
    for _, doc := range textResp.Documents {
        log.Println("text hit:", doc.Content[0].Text)
    }

    // Search the image slot. Your image embedder produces a vector from image
    // bytes / a path / etc., depending on the plugin you use.
    imageResp, err := imageRetriever.Retrieve(ctx, &ai.RetrieverRequest{
        Query:   ai.DocumentFromText("/path/to/query.jpg", nil),
        Options: &qdrantplugin.RetrieverOptions{K: 5},
    })
    if err != nil {
        log.Fatal(err)
    }
    for _, doc := range imageResp.Documents {
        log.Println("image hit:", doc.Content[0].Text)
    }
}
```

## Lookup keys

For named-vector collections, the lookup key is `<collection>/<vector_name>`:

```go
qdrantplugin.Retriever(g, "multi_modal/text")
qdrantplugin.Indexer(g, "multi_modal/image")
```

For single-vector collections, just the collection name:

```go
qdrantplugin.Retriever(g, "my_collection")
```

## Indexing into one slot

The indexer for a specific slot writes only to that slot. To populate both slots for the same logical record, run two indexing calls — one against each slot — in your ingestion pipeline.

```go
textIndexer  := qdrantplugin.Indexer(g, "multi_modal/text")
imageIndexer := qdrantplugin.Indexer(g, "multi_modal/image")

// Same point, two slots.
_ = textIndexer.Index(ctx,  []*ai.Document{textDoc})
_ = imageIndexer.Index(ctx, []*ai.Document{imageDoc})
```

## Tradeoffs

- **Pro**: one collection, one set of payload, one set of filters across modalities.
- **Pro**: per-slot distance functions and dimensions.
- **Con**: each indexed point uses storage for every slot you populate; sparse population is fine, but plan for the upper bound.
- **Con**: more moving parts in the ingestion pipeline.

## See also

- Working code in `examples/named-vectors/`.
- [Configuration reference](configuration.md) — `VectorName` field details.
- [Filtering](filtering.md) — filters work the same regardless of which slot you query.
