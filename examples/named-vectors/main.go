// Example: collection with multiple named vector slots.
//
// Use case: a single collection storing both text and image embeddings, each
// in its own named vector slot with possibly different dimensions. Common
// when you want one logical "library" but separate retrievers per modality.
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

	var textEmbedder, imageEmbedder ai.Embedder // wire up via your plugins

	g := genkit.Init(ctx,
		genkit.WithPlugins(&qdrantplugin.Qdrant{
			Configs: []qdrantplugin.Config{
				{
					CollectionName: "media_library",
					ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6334},
					Embedder:       textEmbedder,
					VectorName:     "text",
				},
				{
					CollectionName: "media_library",
					ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6334},
					Embedder:       imageEmbedder,
					VectorName:     "image",
				},
			},
		}),
	)

	// Retrieve text-similar documents
	textRetriever := qdrantplugin.Retriever(g, "media_library/text")
	resp, err := textRetriever.Retrieve(ctx, &ai.RetrieverRequest{
		Query:   ai.DocumentFromText("how does X work", nil),
		Options: &qdrantplugin.RetrieverOptions{K: 5},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, doc := range resp.Documents {
		fmt.Println(doc.Content[0].Text)
	}

	// You'd use the imageRetriever similarly to find images by query text
	// (CLIP-style cross-modal) or by image features.
	_ = qdrantplugin.Retriever(g, "media_library/image")
}
