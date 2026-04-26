// Integration test exercising the full Init → Index → Retrieve cycle against
// a real Qdrant instance spun up via testcontainers. Skipped under -short and
// when Docker is unavailable.

package qdrant

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	qclient "github.com/qdrant/go-client/qdrant"
	"github.com/testcontainers/testcontainers-go"
	tcqdrant "github.com/testcontainers/testcontainers-go/modules/qdrant"
)

// fakeEmbedder produces deterministic vectors so we can verify the round trip
// without depending on a hosted embedding model. It writes the byte sum of
// the input text into the first vector slot; remaining slots are zero. With
// cosine distance, this keeps all positive vectors aligned to the same
// direction so retrieval returns every doc in a small corpus.
type fakeEmbedder struct {
	dims int
}

func (e *fakeEmbedder) Name() string { return "fake/embedder" }

func (e *fakeEmbedder) Embed(_ context.Context, req *ai.EmbedRequest) (*ai.EmbedResponse, error) {
	out := &ai.EmbedResponse{Embeddings: make([]*ai.Embedding, 0, len(req.Input))}
	for _, d := range req.Input {
		var sum float32
		for _, p := range d.Content {
			for _, b := range []byte(p.Text) {
				sum += float32(b)
			}
		}
		v := make([]float32, e.dims)
		v[0] = sum
		out.Embeddings = append(out.Embeddings, &ai.Embedding{Embedding: v})
	}
	return out, nil
}

// Register is a no-op: we never register this embedder with a Genkit
// registry, the plugin invokes Embed directly via the Embedder field.
func (e *fakeEmbedder) Register(api.Registry) {}

// Compile-time check that fakeEmbedder satisfies ai.Embedder.
var _ ai.Embedder = (*fakeEmbedder)(nil)

func TestIntegration_IndexAndRetrieve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	if !dockerAvailable() {
		t.Skip("skipping integration test: Docker not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tcqdrant.Run(ctx, "qdrant/qdrant:v1.12.4")
	if err != nil {
		// Pulling the image or starting the container can fail in CI
		// without a working Docker daemon. Skip rather than fail.
		t.Skipf("qdrant container failed to start: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Logf("terminate qdrant: %v", err)
		}
	})

	host, port := grpcEndpoint(ctx, t, container)

	// Pre-create the collection. The plugin does not provision schema; that's
	// the caller's responsibility, the same as for the JS plugin.
	rawClient, err := qclient.NewClient(&qclient.Config{Host: host, Port: port})
	if err != nil {
		t.Fatalf("qdrant client: %v", err)
	}
	defer rawClient.Close()

	const collection = "test_collection"
	if err := rawClient.CreateCollection(ctx, &qclient.CreateCollection{
		CollectionName: collection,
		VectorsConfig: qclient.NewVectorsConfig(&qclient.VectorParams{
			Size:     4,
			Distance: qclient.Distance_Cosine,
		}),
	}); err != nil {
		t.Fatalf("create collection: %v", err)
	}

	embedder := &fakeEmbedder{dims: 4}
	g := genkit.Init(ctx,
		genkit.WithPlugins(&Qdrant{
			Configs: []Config{{
				CollectionName: collection,
				ClientParams:   ClientParams{Host: host, Port: port},
				Embedder:       embedder,
			}},
		}),
	)

	indexer := Indexer(g, collection)
	if indexer == nil {
		t.Fatal("Indexer not registered")
	}

	docs := []*ai.Document{
		ai.DocumentFromText("alpha", map[string]any{"i": 1, "src": "test"}),
		ai.DocumentFromText("beta", map[string]any{"i": 2, "src": "test"}),
		ai.DocumentFromText("gamma", map[string]any{"i": 3, "src": "test"}),
	}
	if err := indexer.Index(ctx, docs); err != nil {
		t.Fatalf("Index: %v", err)
	}

	retr := Retriever(g, collection)
	if retr == nil {
		t.Fatal("Retriever not registered")
	}
	resp, err := retr.Retrieve(ctx, &ai.RetrieverRequest{
		Query:   ai.DocumentFromText("alpha", nil),
		Options: &RetrieverOptions{K: 3},
	})
	if err != nil {
		t.Fatalf("Retrieve: %v", err)
	}
	if len(resp.Documents) == 0 {
		t.Fatal("no documents returned")
	}
	seen := map[string]bool{}
	for _, d := range resp.Documents {
		if len(d.Content) > 0 {
			seen[d.Content[0].Text] = true
		}
	}
	if !seen["alpha"] || !seen["beta"] || !seen["gamma"] {
		t.Errorf("expected all three docs back, got %v", seen)
	}
	// Verify metadata round-tripped.
	for _, d := range resp.Documents {
		if _, ok := d.Metadata["_score"]; !ok {
			t.Errorf("expected _score in metadata, got %v", d.Metadata)
		}
	}

	// Filtering: only documents where metadata.src == "test"
	respFiltered, err := retr.Retrieve(ctx, &ai.RetrieverRequest{
		Query: ai.DocumentFromText("alpha", nil),
		Options: &RetrieverOptions{
			K: 3,
			Filter: map[string]any{
				"must": []map[string]any{
					{"key": "metadata.src", "match": map[string]any{"value": "test"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Retrieve(filtered): %v", err)
	}
	if len(respFiltered.Documents) != 3 {
		t.Errorf("filtered: got %d docs, want 3", len(respFiltered.Documents))
	}
}

// dockerAvailable performs a lightweight probe for the Docker socket. If the
// probe fails we skip the integration test rather than burn time waiting for
// testcontainers to time out.
func dockerAvailable() bool {
	for _, sock := range []string{"/var/run/docker.sock"} {
		conn, err := net.DialTimeout("unix", sock, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	return false
}

// grpcEndpoint extracts the host and gRPC port from a running qdrant
// container. The qdrant testcontainers module returns "host:port".
func grpcEndpoint(ctx context.Context, t *testing.T, c *tcqdrant.QdrantContainer) (string, int) {
	t.Helper()
	endpoint, err := c.GRPCEndpoint(ctx)
	if err != nil {
		t.Fatalf("grpc endpoint: %v", err)
	}
	host, portStr, err := net.SplitHostPort(strings.TrimPrefix(endpoint, "http://"))
	if err != nil {
		t.Fatalf("parse endpoint %q: %v", endpoint, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port %q: %v", portStr, err)
	}
	return host, port
}
