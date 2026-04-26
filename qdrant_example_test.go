// Package qdrant_test contains godoc examples for the qdrant-genkit-go plugin.
// These are compile-only examples; they don't run during `go test` (no
// `// Output:` line), but they appear in godoc and are checked for syntax.
package qdrant_test

import (
	"context"
	"log"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"

	qdrantplugin "github.com/arnstarn/qdrant-genkit-go"
)

// Configuring the plugin against a local Qdrant instance.
func ExampleQdrant() {
	ctx := context.Background()

	var embedder ai.Embedder // wire up via your embedding plugin

	g := genkit.Init(ctx,
		genkit.WithPlugins(&qdrantplugin.Qdrant{
			Configs: []qdrantplugin.Config{{
				CollectionName: "my_collection",
				ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6334},
				Embedder:       embedder,
			}},
		}),
	)
	_ = g
}

// Retrieving documents semantically.
func ExampleRetriever() {
	ctx := context.Background()
	var g *genkit.Genkit // returned by genkit.Init(...)

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
}

// Retrieving with a payload filter — only "go" documents indexed in 2025+.
func ExampleRetriever_withFilter() {
	ctx := context.Background()
	var g *genkit.Genkit

	retriever := qdrantplugin.Retriever(g, "my_collection")

	filter := map[string]any{
		"must": []map[string]any{
			{"key": "metadata.lang", "match": map[string]any{"value": "go"}},
			{"key": "metadata.year", "range": map[string]any{"gte": 2025}},
		},
	}

	_, err := retriever.Retrieve(ctx, &ai.RetrieverRequest{
		Query: ai.DocumentFromText("context cancellation patterns", nil),
		Options: &qdrantplugin.RetrieverOptions{
			K:      5,
			Filter: filter,
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

// Indexing documents into a collection.
func ExampleIndexer() {
	ctx := context.Background()
	var g *genkit.Genkit

	indexer := qdrantplugin.Indexer(g, "my_collection")

	docs := []*ai.Document{
		ai.DocumentFromText("Qdrant is a high-performance vector database.", map[string]any{"source": "intro"}),
		ai.DocumentFromText("Genkit is an open-source AI framework.", map[string]any{"source": "intro"}),
	}

	if err := indexer.Index(ctx, docs); err != nil {
		log.Fatal(err)
	}
}

// Configuring two named-vector slots in one collection.
func ExampleQdrant_namedVectors() {
	ctx := context.Background()

	var textEmbedder, imageEmbedder ai.Embedder

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

	textRetriever := qdrantplugin.Retriever(g, "multi_modal/text")
	imageRetriever := qdrantplugin.Retriever(g, "multi_modal/image")
	_, _ = textRetriever, imageRetriever
}
