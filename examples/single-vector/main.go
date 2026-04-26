// Example: single-vector collection with metadata.
//
// Demonstrates the simplest usage: one collection, one vector slot, indexed
// documents with metadata, retrieved by semantic similarity.
//
// Prerequisites:
//   - A running Qdrant instance (e.g., docker run -p 6333:6333 qdrant/qdrant)
//   - An embedder configured via your chosen Genkit plugin (e.g., googleai,
//     openai-compatible)
//   - QDRANT_API_KEY environment variable if your Qdrant requires it
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	qdrantplugin "github.com/arnstarn/qdrant-genkit-go"
)

func main() {
	ctx := context.Background()

	var embedder ai.Embedder // TODO: configure via your embedding plugin

	g, err := genkit.Init(ctx,
		genkit.WithPlugins(&qdrantplugin.Qdrant{
			Configs: []qdrantplugin.Config{{
				CollectionName: "my_collection",
				ClientParams: qdrantplugin.ClientParams{
					Host:   "localhost",
					Port:   6333,
					APIKey: os.Getenv("QDRANT_API_KEY"),
				},
				Embedder: embedder,
			}},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Index documents
	indexer := qdrantplugin.Indexer(g, "my_collection")
	docs := []*ai.Document{
		ai.DocumentFromText("Genkit is an open-source AI framework.", map[string]any{"source": "docs"}),
		ai.DocumentFromText("Qdrant is a high-performance vector database.", map[string]any{"source": "docs"}),
	}
	if err := ai.Index(ctx, indexer, ai.WithIndexerDocs(docs...)); err != nil {
		log.Fatal(err)
	}

	// Retrieve
	retriever := qdrantplugin.Retriever(g, "my_collection")
	resp, err := ai.Retrieve(ctx, retriever,
		ai.WithRetrieverOpts(&ai.RetrieverOptions{K: 3}),
		ai.WithRetrieverText("vector database"),
	)
	if err != nil {
		log.Fatal(err)
	}

	for i, doc := range resp.Documents {
		fmt.Printf("[%d] %s\n", i+1, doc.Content[0].Text)
	}
}
