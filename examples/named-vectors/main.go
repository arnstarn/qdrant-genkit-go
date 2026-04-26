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

	g, err := genkit.Init(ctx,
		genkit.WithPlugins(&qdrantplugin.Qdrant{
			Configs: []qdrantplugin.Config{
				{
					CollectionName: "media_library",
					ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6333},
					Embedder:       textEmbedder,
					VectorName:     "text",
				},
				{
					CollectionName: "media_library",
					ClientParams:   qdrantplugin.ClientParams{Host: "localhost", Port: 6333},
					Embedder:       imageEmbedder,
					VectorName:     "image",
				},
			},
		}),
	)
	if err != nil {
		log.Fatal(err)
	}

	// Retrieve text-similar documents
	textRetriever := qdrantplugin.Retriever(g, "media_library/text")
	resp, err := ai.Retrieve(ctx, textRetriever,
		ai.WithRetrieverOpts(&ai.RetrieverOptions{K: 5}),
		ai.WithRetrieverText("how does X work"),
	)
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
