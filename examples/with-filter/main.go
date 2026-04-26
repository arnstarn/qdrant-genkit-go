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

	g := genkit.Init(ctx,
		genkit.WithPlugins(&qdrantplugin.Qdrant{
			Configs: []qdrantplugin.Config{{
				CollectionName: "docs",
				ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6334},
				Embedder:       embedder,
			}},
		}),
	)

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

	resp, err := retriever.Retrieve(ctx, &ai.RetrieverRequest{
		Query: ai.DocumentFromText("how to handle context cancellation", nil),
		Options: &qdrantplugin.RetrieverOptions{
			K:      5,
			Filter: filter,
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	for i, doc := range resp.Documents {
		fmt.Printf("[%d] %s\n", i+1, doc.Content[0].Text)
	}
}
