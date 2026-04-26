// Package client wraps construction of the official Qdrant Go gRPC client.
//
// It exists to keep the public package's surface tiny and to make the Qdrant
// connection setup easy to swap or stub in tests.
package client

import (
	"fmt"

	qclient "github.com/qdrant/go-client/qdrant"
)

// Params configures how the plugin reaches a Qdrant instance. It mirrors the
// public ClientParams type but lives here to keep import paths clean.
type Params struct {
	Host   string
	Port   int
	APIKey string
	UseTLS bool
}

// New returns a connected Qdrant client. The official Go client uses gRPC; the
// default port is 6334 (not the REST port 6333). Callers may pass either.
func New(p Params) (*qclient.Client, error) {
	host := p.Host
	if host == "" {
		host = "localhost"
	}
	port := p.Port
	if port == 0 {
		port = 6334
	}

	c, err := qclient.NewClient(&qclient.Config{
		Host:   host,
		Port:   port,
		APIKey: p.APIKey,
		UseTLS: p.UseTLS,
	})
	if err != nil {
		return nil, fmt.Errorf("qdrant: failed to create client for %s:%d: %w", host, port, err)
	}
	return c, nil
}
