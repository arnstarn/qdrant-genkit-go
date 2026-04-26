// Example: retrieval with metadata filtering.
//
// Qdrant supports rich filtering on payload metadata. Pass a filter map to the
// retriever to scope results to documents matching specific criteria —
// language, source, time range, etc.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/genkit"
	qdrantplugin "github.com/arnstarn/qdrant-genkit-go"
)

func main() {
	ctx := context.Background()

	var embedder ai.Embedder // wire up via your embedding plugin

	g, err := genkit.Init(ctx,
		genkit.WithPlugins(&qdrantplugin.Qdrant{
			Configs: []qdrantplugin.Config{{
				CollectionName: "docs",
				ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6333},
				Embedder:       embedder,
			}},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	retriever := qdrantplugin.Retriever(g, "docs")

	// Filter: only documents where metadata.lang == "go" AND metadata.year >= 2024
	filter := map[string]any{
		"must": []map[string]any{
			{
				"key":   "metadata.lang",
				"match": map[string]any{"value": "go"},
			},
			{
				"key":   "metadata.year",
				"range": map[string]any{"gte": 2024},
			},
		},
	}

	resp, err := ai.Retrieve(ctx, retriever,
		ai.WithRetrieverOpts(&ai.RetrieverOptions{
			K:      5,
			Filter: filter,
		}),
		ai.WithRetrieverText("how to handle context cancellation"),
	)
	if err != nil {
		log.Fatal(err)
	}

	for i, doc := range resp.Documents {
		fmt.Printf("[%d] %s\n", i+1, doc.Content[0].Text)
	}
}
