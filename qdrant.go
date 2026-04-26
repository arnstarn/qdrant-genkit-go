// Package qdrant provides a Firebase Genkit Go plugin for the Qdrant vector
// database. It exposes Indexer and Retriever implementations that integrate
// with any Genkit Embedder.
//
// Mirror of the JS/TS @genkit-ai/qdrant plugin's API where reasonable.
package qdrant

import (
	"context"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
)

// Qdrant is the Genkit plugin entry point. Pass to genkit.WithPlugins.
type Qdrant struct {
	// Configs declares one or more (collection, vector) configurations.
	// For named-vector collections, declare one Config per vector slot you
	// want to expose as a retriever/indexer.
	Configs []Config
}

// Config configures a single retriever/indexer pair targeting one Qdrant
// collection (and optionally one named vector slot inside it).
type Config struct {
	// CollectionName is the Qdrant collection name. Required.
	CollectionName string

	// ClientParams configures the connection to Qdrant.
	ClientParams ClientParams

	// Embedder is the Genkit embedder used for both indexing and retrieval
	// queries. Required.
	Embedder ai.Embedder

	// ContentPayloadKey is the payload key holding document text.
	// Default: "content".
	ContentPayloadKey string

	// MetadataPayloadKey is the payload key holding metadata.
	// Default: "metadata".
	MetadataPayloadKey string

	// VectorName is the name of the vector slot in named-vector collections.
	// Leave empty for single-vector collections.
	VectorName string
}

// ClientParams describes how to reach a Qdrant instance.
type ClientParams struct {
	Host   string // default: "localhost"
	Port   int    // default: 6333
	APIKey string // optional
	UseTLS bool   // default: false
}

// Name returns the plugin name as required by the Genkit plugin interface.
func (q *Qdrant) Name() string { return "qdrant" }

// Init registers Indexers and Retrievers for each Config.
// Implementation TODO: see issue #1.
func (q *Qdrant) Init(ctx context.Context, g *genkit.Genkit) error {
	// TODO:
	// 1. For each Config, validate required fields
	// 2. Build a Qdrant Go client (qdrant/go-client)
	// 3. Register a Retriever with key = collectionName (or collectionName/vectorName)
	// 4. Register an Indexer with the same key
	// 5. Return any registration errors
	return nil
}

// Retriever returns the registered retriever for the given collection name.
// For named-vector collections, use the form "collection/vector_name".
func Retriever(g *genkit.Genkit, name string) ai.Retriever {
	return genkit.LookupRetriever(g, "qdrant", name)
}

// Indexer returns the registered indexer for the given collection name.
// For named-vector collections, use the form "collection/vector_name".
func Indexer(g *genkit.Genkit, name string) ai.Indexer {
	return genkit.LookupIndexer(g, "qdrant", name)
}
