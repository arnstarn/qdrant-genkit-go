# Connection patterns

This page covers how the plugin connects to Qdrant: which protocol it uses, what to set on `ClientParams` for self-hosted vs. Qdrant Cloud deployments, TLS, and common connection errors.

## gRPC, not REST

The plugin uses Qdrant's gRPC API exclusively. The official [Qdrant Go client](https://github.com/qdrant/go-client) is gRPC-only; this plugin builds on top of it.

| Port | Protocol | Used by |
|---|---|---|
| `6334` | gRPC | this plugin |
| `6333` | HTTP REST | not used; useful for `curl` admin |

When you set `ClientParams.Port`, set it to the gRPC port (`6334` by default for self-hosted, or whatever your provider documents). The REST port is not used and you should not point the plugin at it.

## Self-hosted Qdrant

```go
ClientParams: qdrantplugin.ClientParams{
    Host: "localhost",
    Port: 6334,
}
```

If you run Qdrant in Docker with `-p 6334:6334`, this is all you need. No API key, no TLS.

If you've enabled `service.api_key` in your Qdrant config:

```go
ClientParams: qdrantplugin.ClientParams{
    Host:   "qdrant.internal",
    Port:   6334,
    APIKey: os.Getenv("QDRANT_API_KEY"),
}
```

## Qdrant Cloud

```go
ClientParams: qdrantplugin.ClientParams{
    Host:   "your-cluster-id.us-east-0-0.aws.cloud.qdrant.io",
    Port:   6334,
    APIKey: os.Getenv("QDRANT_API_KEY"),
    UseTLS: true,
}
```

Qdrant Cloud terminates TLS in front of gRPC, so set `UseTLS: true`. The host is the cluster URL Qdrant Cloud shows in the dashboard.

## TLS for self-hosted

If you put a TLS terminator (Caddy, nginx, Cloudflare) in front of self-hosted Qdrant:

```go
ClientParams: qdrantplugin.ClientParams{
    Host:   "qdrant.example.com",
    Port:   443, // or whatever your terminator listens on
    APIKey: os.Getenv("QDRANT_API_KEY"),
    UseTLS: true,
}
```

mTLS is not yet supported; track [the roadmap](roadmap.md).

## Connection sharing

If you declare multiple `Config` entries pointing at the same `(Host, Port, APIKey, UseTLS)` tuple, the plugin opens **one** Qdrant client and reuses it across all configs. This is intentional — useful when one collection has multiple named vector slots and you declare one `Config` per slot.

## Common errors

### `connection refused`
Qdrant isn't running, or your `Host`/`Port` doesn't match the running instance. Verify:
- `docker ps` shows Qdrant running
- `nc -zv <host> 6334` succeeds
- `curl http://<host>:6333/` returns Qdrant's banner (REST quick-check)

### `Unauthorized`
You're missing `APIKey`, or the key is wrong. For Qdrant Cloud, the key is shown only at cluster creation — keep it in a secret manager.

### `transport: authentication handshake failed`
TLS misconfiguration. Either:
- You set `UseTLS: true` against an instance that doesn't speak TLS — set it to `false`
- You set `UseTLS: false` against a TLS-fronted endpoint — set it to `true`

### `context deadline exceeded`
Network reachability issue, or Qdrant is overloaded. Genkit's default deadlines apply to the surrounding flow, not the gRPC call itself; if you need a tighter Qdrant timeout, wrap the retrieve in a `context.WithTimeout`.

## See also

- [Configuration](configuration.md) — every `ClientParams` field
- [Getting started](getting-started.md) — runnable end-to-end example
- [Roadmap](roadmap.md) — optional REST transport, mTLS
