# Filtering

Qdrant supports rich payload filtering at query time. Pass a filter via `qdrantplugin.RetrieverOptions.Filter` and the plugin forwards it to Qdrant's search API.

The filter shape is exactly the [Qdrant filter JSON](https://qdrant.tech/documentation/concepts/filtering/) expressed as a Go `map[string]any`. This page collects the most common patterns.

## How filter keys work

By default, document metadata is stored under the payload key `metadata` (configurable via `MetadataPayloadKey`). So a metadata field named `lang` is referenced in filters as `metadata.lang`.

If you've set `MetadataPayloadKey: "meta"`, swap `metadata.` for `meta.` in every example below.

## The three top-level clauses

| Clause | Meaning |
|---|---|
| `must` | All conditions must match (AND). |
| `should` | At least one condition must match (OR). |
| `must_not` | None of the conditions may match (NOT). |

You can combine all three, and you can nest filters inside each condition.

## Example 1 — Exact match (AND)

> "Only return Go documents."

```go
filter := map[string]any{
    "must": []map[string]any{
        {"key": "metadata.lang", "match": map[string]any{"value": "go"}},
    },
}

retriever.Retrieve(ctx, &ai.RetrieverRequest{
    Query:   ai.DocumentFromText("how do I configure logging?", nil),
    Options: &qdrantplugin.RetrieverOptions{K: 5, Filter: filter},
})
```

## Example 2 — Multiple ANDs

> "Go documents that are also marked as official."

```go
filter := map[string]any{
    "must": []map[string]any{
        {"key": "metadata.lang",     "match": map[string]any{"value": "go"}},
        {"key": "metadata.official", "match": map[string]any{"value": true}},
    },
}
```

## Example 3 — One-of (OR via `should`)

> "Either Go or Rust."

```go
filter := map[string]any{
    "should": []map[string]any{
        {"key": "metadata.lang", "match": map[string]any{"value": "go"}},
        {"key": "metadata.lang", "match": map[string]any{"value": "rust"}},
    },
}
```

Or, more compactly, with `match.any`:

```go
filter := map[string]any{
    "must": []map[string]any{
        {"key": "metadata.lang", "match": map[string]any{"any": []string{"go", "rust"}}},
    },
}
```

## Example 4 — Exclusion (`must_not`)

> "Anything except deprecated docs."

```go
filter := map[string]any{
    "must_not": []map[string]any{
        {"key": "metadata.deprecated", "match": map[string]any{"value": true}},
    },
}
```

## Example 5 — Range (numeric)

> "Documents with `version` between 2 and 4 inclusive."

```go
filter := map[string]any{
    "must": []map[string]any{
        {
            "key": "metadata.version",
            "range": map[string]any{
                "gte": 2,
                "lte": 4,
            },
        },
    },
}
```

Supported operators: `gt`, `gte`, `lt`, `lte`.

## Example 6 — Range (datetime)

> "Documents indexed in 2025."

Store dates as RFC 3339 strings in metadata, then:

```go
filter := map[string]any{
    "must": []map[string]any{
        {
            "key": "metadata.indexed_at",
            "range": map[string]any{
                "gte": "2025-01-01T00:00:00Z",
                "lt":  "2026-01-01T00:00:00Z",
            },
        },
    },
}
```

## Example 7 — Combined AND/OR/NOT

> "Go OR Rust documents, indexed after 2025-06-01, that are not deprecated."

```go
filter := map[string]any{
    "must": []map[string]any{
        {"key": "metadata.lang", "match": map[string]any{"any": []string{"go", "rust"}}},
        {"key": "metadata.indexed_at", "range": map[string]any{"gte": "2025-06-01T00:00:00Z"}},
    },
    "must_not": []map[string]any{
        {"key": "metadata.deprecated", "match": map[string]any{"value": true}},
    },
}
```

## Example 8 — Full-text match

> "Documents whose `title` contains the substring `tutorial`."

```go
filter := map[string]any{
    "must": []map[string]any{
        {"key": "metadata.title", "match": map[string]any{"text": "tutorial"}},
    },
}
```

For best results on large corpora, configure a [full-text payload index](https://qdrant.tech/documentation/concepts/indexing/#payload-index) on that field server-side.

## Example 9 — Geo

> "Within 10 km of a coordinate."

```go
filter := map[string]any{
    "must": []map[string]any{
        {
            "key": "metadata.location",
            "geo_radius": map[string]any{
                "center": map[string]any{"lat": 37.7749, "lon": -122.4194},
                "radius": 10000.0, // meters
            },
        },
    },
}
```

Qdrant also supports `geo_bounding_box` and `geo_polygon` — see the upstream docs.

## Performance tips

- Create [payload indexes](https://qdrant.tech/documentation/concepts/indexing/#payload-index) on fields you filter on frequently. Without an index, Qdrant filters by full scan, which gets slow as the collection grows.
- For high-cardinality string fields used in `match.value`, prefer the `keyword` payload index. For numeric ranges, use the `integer` or `float` index. For full-text, use `text`.
- Combining many `should` clauses can be slower than one `match.any`. Use `match.any` when listing exact alternatives.

## See also

- Working code in `examples/with-filter/`.
- [Qdrant filtering documentation](https://qdrant.tech/documentation/concepts/filtering/) — the canonical, exhaustive reference.
- [Configuration reference](configuration.md) — `MetadataPayloadKey` field, which controls the prefix used in filter keys.
