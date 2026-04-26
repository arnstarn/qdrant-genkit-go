# API reference

Public types and functions exported by `github.com/arnstarn/qdrant-genkit-go` (package `qdrant`).

For runnable examples see the [`examples/`](../examples/) directory. For configuration field-by-field details see [configuration.md](configuration.md). The canonical source of truth is the godoc generated from `qdrant.go`.

## Package import

```go
import qdrantplugin "github.com/arnstarn/qdrant-genkit-go"
```

The package name is `qdrant`; this guide aliases it to `qdrantplugin` to avoid colliding with the upstream Qdrant Go client.

---

## Types

### `Qdrant`

```go
type Qdrant struct {
    Configs []Config
}
```

The plugin entry point. Pass to `genkit.WithPlugins(...)` when initializing Genkit.

`Configs` declares one or more `(collection, vector)` configurations. Each entry registers a paired indexer and retriever.

### `Config`

```go
type Config struct {
    CollectionName     string       // required
    ClientParams       ClientParams // required (Host/Port have defaults)
    Embedder           ai.Embedder  // required
    ContentPayloadKey  string       // default: "content"
    MetadataPayloadKey string       // default: "metadata"
    VectorName         string       // default: "" (single-vector collection)
}
```

Configures a single retriever/indexer pair targeting one Qdrant collection (and optionally one named vector slot inside it).

See [configuration.md](configuration.md) for the full per-field guide.

### `ClientParams`

```go
type ClientParams struct {
    Host   string // default: "localhost"
    Port   int    // default: 6334 (gRPC)
    APIKey string // default: "" (no auth)
    UseTLS bool   // default: false
}
```

Connection parameters for a Qdrant instance. The plugin uses Qdrant's gRPC API; the REST port (`6333`) is not used.

### `RetrieverOptions`

```go
type RetrieverOptions struct {
    K              int      // default: 3
    Filter         any      // map[string]any or *qdrant.Filter
    ScoreThreshold *float32 // optional minimum similarity score
}
```

Pass via `RetrieverRequest.Options`. See [filtering.md](filtering.md) for `Filter` examples.

### `IndexerHandle`

```go
type IndexerHandle interface {
    Name() string
    Index(ctx context.Context, docs []*ai.Document) error
}
```

Returned by `Indexer(g, name)`. Genkit Go 1.x does not yet expose a first-class `ai.Indexer` interface; this minimal interface fills that gap and will align to Genkit's once available.

---

## Functions

### `(*Qdrant) Name() string`

Returns the plugin name (`"qdrant"`). Implements the Genkit plugin interface; you don't normally call this yourself.

### `(*Qdrant) Init(ctx context.Context) []api.Action`

Validates each `Config`, opens Qdrant clients, and returns the registered Retriever actions. Called by Genkit during `genkit.Init` when the plugin is passed via `genkit.WithPlugins`. You don't normally call this yourself.

### `Retriever(g *genkit.Genkit, name string) ai.Retriever`

Returns the registered retriever for a collection.

- For single-vector collections, `name` is the collection name (e.g., `"my_collection"`).
- For named-vector collections, `name` is `"<collection>/<vector_name>"` (e.g., `"multi_modal/text"`).

The returned `ai.Retriever` is the standard Genkit type. Call `Retrieve(ctx, *ai.RetrieverRequest)` on it.

### `Indexer(g *genkit.Genkit, name string) IndexerHandle`

Returns the registered indexer for a collection. Naming rules match `Retriever`. Call `Index(ctx, []*ai.Document)` on the result.

---

## Calling the plugin

### Indexing

```go
indexer := qdrantplugin.Indexer(g, "my_collection")
err := indexer.Index(ctx, docs) // docs is []*ai.Document
```

### Retrieving

```go
retriever := qdrantplugin.Retriever(g, "my_collection")
resp, err := retriever.Retrieve(ctx, &ai.RetrieverRequest{
    Query: ai.DocumentFromText("query string", nil),
    Options: &qdrantplugin.RetrieverOptions{
        K:      5,
        Filter: filterMap, // optional; see filtering.md
    },
})
```

`resp.Documents` is a `[]*ai.Document` ordered by similarity. The similarity score is preserved at `doc.Metadata["_score"]`.

---

## Naming-key cheat sheet

| Collection layout | Lookup key |
|---|---|
| Single-vector | `"<collection>"` |
| Named-vector | `"<collection>/<vector_name>"` |

---

## See also

- [Getting started](getting-started.md)
- [Configuration](configuration.md)
- [Named vectors](named-vectors.md)
- [Filtering](filtering.md)
- Source: [`qdrant.go`](../qdrant.go)
