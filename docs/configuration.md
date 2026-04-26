# Configuration

Every field on `Qdrant`, `Config`, and `ClientParams`, what it does, its default, and when you should override it.

## `Qdrant`

The plugin entry point. Pass to `genkit.WithPlugins`.

```go
type Qdrant struct {
    Configs []Config
}
```

### `Configs`

A slice of `Config` values. Each entry registers exactly one `(Indexer, Retriever)` pair keyed by the target collection (and optional vector slot).

You declare multiple `Config` entries to:
- Address several distinct collections from one Genkit app.
- Address several named-vector slots inside a single collection (e.g., `text` + `image`).

For each `Config`, the plugin registers an indexer and a retriever you look up via `qdrantplugin.Indexer(g, name)` / `qdrantplugin.Retriever(g, name)`. The lookup `name` is:
- `<CollectionName>` for single-vector collections.
- `<CollectionName>/<VectorName>` for named-vector collections.

## `Config`

Configures one indexer/retriever pair.

```go
type Config struct {
    CollectionName     string
    ClientParams       ClientParams
    Embedder           ai.Embedder
    ContentPayloadKey  string
    MetadataPayloadKey string
    VectorName         string
}
```

### `CollectionName` (required)

The Qdrant collection name. Must already exist — the plugin does not create collections.

Override: always.

### `ClientParams` (required)

Connection details for the Qdrant instance backing this config. See [`ClientParams`](#clientparams) below.

You can mix collections from different Qdrant instances by giving each `Config` its own `ClientParams`.

### `Embedder` (required)

The Genkit `ai.Embedder` used for both indexing (turning incoming documents into vectors) and retrieval (turning query text into a vector).

Override: always. This is the model you wire up via your embedding plugin (Vertex AI, Google AI, OpenAI-compatible, etc.).

> **Important**: queries and stored vectors must come from the *same* embedder. If you re-embed your corpus with a different model, you must re-index everything.

### `ContentPayloadKey`

The Qdrant payload key under which the document's text is stored.

- **Default**: `"content"`
- **Override when**: you're sharing a collection with another system that already uses a different key, or you want a more domain-specific name (e.g., `"body"`, `"text"`, `"chunk"`).

### `MetadataPayloadKey`

The Qdrant payload key under which the document's metadata map is stored.

- **Default**: `"metadata"`
- **Override when**: you're sharing a collection with another system that uses a different convention (e.g., `"meta"`, `"attrs"`).

> Filter expressions reference these keys. If you change `MetadataPayloadKey` to `"meta"`, your filter clauses must use `"key": "meta.lang"` instead of `"key": "metadata.lang"`. See [filtering.md](filtering.md).

### `VectorName`

The name of the vector slot in a [named-vector collection](named-vectors.md).

- **Default**: `""` — meaning the collection is single-vector.
- **Override when**: your collection was created with named vectors (e.g., `"text"`, `"code"`, `"image"`).

For named-vector collections you'll typically declare one `Config` per slot, all sharing the same `CollectionName` but with different `VectorName` and `Embedder` values.

## `ClientParams`

Describes how to reach a Qdrant instance.

```go
type ClientParams struct {
    Host   string
    Port   int
    APIKey string
    UseTLS bool
}
```

### `Host`

- **Default**: `"localhost"`
- **Override when**: you're using Qdrant Cloud, a remote self-hosted instance, or a Docker-network hostname.

### `Port`

- **Default**: `6334` (gRPC)
- **Override when**: you've changed Qdrant's default ports, or your hosted provider uses a different port.

> The plugin uses Qdrant's gRPC API (port `6334` by default). Qdrant also exposes a REST API on `6333` for ad-hoc curl/admin tasks; that port is not used by this plugin.

### `APIKey`

- **Default**: `""` (no authentication)
- **Override when**: your Qdrant instance is configured with `service.api_key` (Qdrant Cloud always requires this; self-hosted optionally).

Avoid hardcoding. Pull from env or a secrets manager:

```go
ClientParams: qdrantplugin.ClientParams{
    Host:   os.Getenv("QDRANT_HOST"),
    Port:   6334,
    APIKey: os.Getenv("QDRANT_API_KEY"),
    UseTLS: true,
},
```

### `UseTLS`

- **Default**: `false`
- **Override when**: connecting to Qdrant Cloud or any TLS-fronted endpoint. Set to `true` and use the port your endpoint exposes (Qdrant Cloud terminates TLS in front of gRPC).

## Quick reference table

| Field | Type | Default | Required |
|---|---|---|---|
| `CollectionName` | string | — | yes |
| `ClientParams.Host` | string | `localhost` | no |
| `ClientParams.Port` | int | `6334` (gRPC) | no |
| `ClientParams.APIKey` | string | `""` | no |
| `ClientParams.UseTLS` | bool | `false` | no |
| `Embedder` | `ai.Embedder` | — | yes |
| `ContentPayloadKey` | string | `content` | no |
| `MetadataPayloadKey` | string | `metadata` | no |
| `VectorName` | string | `""` | no (required for named-vector collections) |
