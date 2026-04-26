package qdrant

import (
	"context"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	qclient "github.com/qdrant/go-client/qdrant"
)

// stubEmbedder is the smallest possible ai.Embedder implementation, just
// enough for unit tests that exercise plugin wiring without going to a real
// embedding model.
type stubEmbedder struct{}

func (stubEmbedder) Name() string { return "stub" }
func (stubEmbedder) Embed(_ context.Context, req *ai.EmbedRequest) (*ai.EmbedResponse, error) {
	out := &ai.EmbedResponse{Embeddings: make([]*ai.Embedding, len(req.Input))}
	for i := range req.Input {
		out.Embeddings[i] = &ai.Embedding{Embedding: []float32{0.1, 0.2}}
	}
	return out, nil
}
func (stubEmbedder) Register(api.Registry) {} //nolint:revive

func TestApplyConfigDefaults_FillsKeys(t *testing.T) {
	cfg := &Config{}
	applyConfigDefaults(cfg)
	if cfg.ContentPayloadKey != DefaultContentPayloadKey {
		t.Errorf("ContentPayloadKey = %q, want %q", cfg.ContentPayloadKey, DefaultContentPayloadKey)
	}
	if cfg.MetadataPayloadKey != DefaultMetadataPayloadKey {
		t.Errorf("MetadataPayloadKey = %q, want %q", cfg.MetadataPayloadKey, DefaultMetadataPayloadKey)
	}
}

func TestApplyConfigDefaults_PreservesUserValues(t *testing.T) {
	cfg := &Config{
		ContentPayloadKey:  "body",
		MetadataPayloadKey: "meta",
	}
	applyConfigDefaults(cfg)
	if cfg.ContentPayloadKey != "body" {
		t.Errorf("ContentPayloadKey = %q, want body", cfg.ContentPayloadKey)
	}
	if cfg.MetadataPayloadKey != "meta" {
		t.Errorf("MetadataPayloadKey = %q, want meta", cfg.MetadataPayloadKey)
	}
}

func TestValidateConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid", Config{CollectionName: "c", Embedder: stubEmbedder{}}, false},
		{"missing collection", Config{Embedder: stubEmbedder{}}, true},
		{"missing embedder", Config{CollectionName: "c"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConfig(&tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("err=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestRegisterName(t *testing.T) {
	tests := []struct {
		collection, vector, want string
	}{
		{"docs", "", "docs"},
		{"docs", "text", "docs/text"},
	}
	for _, tt := range tests {
		if got := registerName(tt.collection, tt.vector); got != tt.want {
			t.Errorf("registerName(%q, %q) = %q, want %q", tt.collection, tt.vector, got, tt.want)
		}
	}
}

func TestOptsFromRequest_Defaults(t *testing.T) {
	got := optsFromRequest(&ai.RetrieverRequest{})
	if got.K != 3 {
		t.Errorf("K = %d, want 3", got.K)
	}
}

func TestOptsFromRequest_PointerOptions(t *testing.T) {
	got := optsFromRequest(&ai.RetrieverRequest{Options: &RetrieverOptions{K: 7}})
	if got.K != 7 {
		t.Errorf("K = %d, want 7", got.K)
	}
}

func TestOptsFromRequest_ValueOptions(t *testing.T) {
	got := optsFromRequest(&ai.RetrieverRequest{Options: RetrieverOptions{K: 9}})
	if got.K != 9 {
		t.Errorf("K = %d, want 9", got.K)
	}
}

func TestBuildFilter_Passthrough(t *testing.T) {
	f := &qclient.Filter{}
	got, err := buildFilter(f)
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	if got != f {
		t.Errorf("expected pointer passthrough")
	}
}

func TestBuildFilter_Map(t *testing.T) {
	got, err := buildFilter(map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"value": "v"}},
		},
	})
	if err != nil {
		t.Fatalf("buildFilter: %v", err)
	}
	if got == nil || len(got.Must) != 1 {
		t.Errorf("expected one Must condition, got %v", got)
	}
}

func TestBuildFilter_Nil(t *testing.T) {
	got, err := buildFilter(nil)
	if err != nil {
		t.Fatalf("buildFilter(nil): %v", err)
	}
	if got != nil {
		t.Errorf("expected nil filter")
	}
}

func TestBuildFilter_BadType(t *testing.T) {
	if _, err := buildFilter(123); err == nil {
		t.Errorf("expected error for bogus filter type")
	}
}
