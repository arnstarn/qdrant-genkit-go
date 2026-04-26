// Unit tests that don't require Docker / a live Qdrant instance. These
// complement qdrant_test.go (smaller helpers) and qdrant_integration_test.go
// (the full Init→Index→Retrieve cycle gated behind testcontainers).
//
// Coverage focus:
//   - validateConfig edge cases.
//   - applyConfigDefaults idempotence.
//   - clientFor dedup / reuse semantics.
//   - registerName.
//   - Public Retriever / Indexer / Index lookup wrappers.
//   - optsFromRequest fallbacks (nil Options, negative K, typed-nil pointer).
//   - retrieverImpl.retrieve and indexerImpl.Index error paths that don't
//     require a real Qdrant connection (caught before the gRPC call).

package qdrant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	qclient "github.com/qdrant/go-client/qdrant"
)

// failingEmbedder is an embedder that always returns the configured error so
// tests can hit the error branches in retrieve / Index without a real model.
type failingEmbedder struct {
	err error
}

func (f *failingEmbedder) Name() string                                   { return "fail" }
func (f *failingEmbedder) Register(api.Registry)                          {} //nolint:revive
func (f *failingEmbedder) Embed(_ context.Context, _ *ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return nil, f.err
}

// emptyEmbedder returns an empty Embeddings slice regardless of input — used
// to trigger the "no embeddings" path inside retrieve.
type emptyEmbedder struct{}

func (emptyEmbedder) Name() string          { return "empty" }
func (emptyEmbedder) Register(api.Registry) {} //nolint:revive
func (emptyEmbedder) Embed(_ context.Context, _ *ai.EmbedRequest) (*ai.EmbedResponse, error) {
	return &ai.EmbedResponse{Embeddings: nil}, nil
}

// mismatchEmbedder returns one fewer embedding than there are input docs so
// the indexer's count check fires.
type mismatchEmbedder struct{}

func (mismatchEmbedder) Name() string          { return "mismatch" }
func (mismatchEmbedder) Register(api.Registry) {} //nolint:revive
func (mismatchEmbedder) Embed(_ context.Context, req *ai.EmbedRequest) (*ai.EmbedResponse, error) {
	out := &ai.EmbedResponse{}
	if len(req.Input) > 1 {
		out.Embeddings = []*ai.Embedding{{Embedding: []float32{0.1}}}
	}
	return out, nil
}

// ----------------------------------------------------------------------------
// validateConfig — exhaustive error branches.
// ----------------------------------------------------------------------------

func TestValidateConfig_Errors(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantSub string
	}{
		{"empty collection", Config{Embedder: stubEmbedder{}}, "CollectionName"},
		{"empty embedder", Config{CollectionName: "c"}, "Embedder"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfig(&tc.cfg)
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Errorf("error %q does not contain %q", err, tc.wantSub)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// applyConfigDefaults idempotence.
// ----------------------------------------------------------------------------

func TestApplyConfigDefaults_Idempotent(t *testing.T) {
	cfg := &Config{}
	applyConfigDefaults(cfg)
	first := *cfg
	applyConfigDefaults(cfg)
	if *cfg != first {
		t.Errorf("applyConfigDefaults not idempotent: %v then %v", first, *cfg)
	}
}

// ----------------------------------------------------------------------------
// registerName — also covers the empty-collection edge.
// ----------------------------------------------------------------------------

func TestRegisterName_AllShapes(t *testing.T) {
	cases := []struct {
		col, vec, want string
	}{
		{"docs", "", "docs"},
		{"docs", "text", "docs/text"},
		{"", "", ""},
		{"", "text", "/text"},
	}
	for _, tc := range cases {
		if got := registerName(tc.col, tc.vec); got != tc.want {
			t.Errorf("registerName(%q,%q) = %q, want %q", tc.col, tc.vec, got, tc.want)
		}
	}
}

// ----------------------------------------------------------------------------
// clientFor — reuse, dedup, and per-key separation.
// ----------------------------------------------------------------------------

// newQdrantWithMaps returns a Qdrant whose private maps are initialized so we
// can call clientFor without needing a full Init().
func newQdrantWithMaps() *Qdrant {
	return &Qdrant{
		clients:  make(map[string]*qclient.Client),
		indexers: make(map[string]*indexerImpl),
	}
}

func TestClientFor_ReusesSameKey(t *testing.T) {
	q := newQdrantWithMaps()
	cp := ClientParams{Host: "localhost", Port: 6334}

	c1, err := q.clientFor(cp)
	if err != nil {
		t.Fatalf("first clientFor: %v", err)
	}
	c2, err := q.clientFor(cp)
	if err != nil {
		t.Fatalf("second clientFor: %v", err)
	}
	if c1 != c2 {
		t.Errorf("expected the same client for identical ClientParams; got distinct pointers")
	}
	if got := len(q.clients); got != 1 {
		t.Errorf("clients map has %d entries, want 1", got)
	}
	c1.Close()
}

func TestClientFor_DistinctKeys(t *testing.T) {
	q := newQdrantWithMaps()
	cases := []ClientParams{
		{Host: "host-a", Port: 6334},
		{Host: "host-a", Port: 6335}, // different port → new client
		{Host: "host-b", Port: 6334}, // different host → new client
		{Host: "host-a", Port: 6334, APIKey: "k1"},
		{Host: "host-a", Port: 6334, APIKey: "k1", UseTLS: true},
	}
	for _, cp := range cases {
		if _, err := q.clientFor(cp); err != nil {
			t.Fatalf("clientFor(%+v): %v", cp, err)
		}
	}
	if got := len(q.clients); got != len(cases) {
		t.Errorf("clients map has %d entries, want %d", got, len(cases))
	}
	for _, c := range q.clients {
		c.Close()
	}
}

// ----------------------------------------------------------------------------
// optsFromRequest — fallback / clamp paths.
// ----------------------------------------------------------------------------

func TestOptsFromRequest_NilOptions(t *testing.T) {
	got := optsFromRequest(&ai.RetrieverRequest{Options: nil})
	if got.K != 3 {
		t.Errorf("K = %d, want default 3", got.K)
	}
}

func TestOptsFromRequest_TypedNilPointer(t *testing.T) {
	// Passing a typed-nil *RetrieverOptions should fall back to the default
	// rather than dereferencing nil.
	var opts *RetrieverOptions
	got := optsFromRequest(&ai.RetrieverRequest{Options: opts})
	if got.K != 3 {
		t.Errorf("K = %d, want default 3 for typed-nil options", got.K)
	}
}

func TestOptsFromRequest_ZeroKClampedToDefault(t *testing.T) {
	got := optsFromRequest(&ai.RetrieverRequest{Options: &RetrieverOptions{K: 0}})
	if got.K != 3 {
		t.Errorf("K = %d, want default 3 when K=0", got.K)
	}
}

func TestOptsFromRequest_NegativeKClampedToDefault(t *testing.T) {
	got := optsFromRequest(&ai.RetrieverRequest{Options: &RetrieverOptions{K: -5}})
	if got.K != 3 {
		t.Errorf("K = %d, want default 3 when K<0", got.K)
	}
}

func TestOptsFromRequest_PassthroughFields(t *testing.T) {
	thr := float32(0.7)
	got := optsFromRequest(&ai.RetrieverRequest{Options: RetrieverOptions{
		K:              5,
		Filter:         map[string]any{"must": []map[string]any{}},
		ScoreThreshold: &thr,
	}})
	if got.K != 5 {
		t.Errorf("K = %d, want 5", got.K)
	}
	if got.ScoreThreshold == nil || *got.ScoreThreshold != 0.7 {
		t.Errorf("ScoreThreshold = %v, want 0.7", got.ScoreThreshold)
	}
	if got.Filter == nil {
		t.Errorf("Filter dropped: %v", got.Filter)
	}
}

// ----------------------------------------------------------------------------
// buildFilter — value Filter passthrough (the existing test covers the
// pointer form; this exercises the by-value branch).
// ----------------------------------------------------------------------------

// (No by-value Filter passthrough test: qclient.Filter embeds a sync.Mutex via
// the protobuf message machinery, so even constructing a fresh value to pass
// in trips `go vet`'s copylocks check. The pointer-passthrough branch is
// already exercised in TestBuildFilter_Passthrough; the by-value branch will
// be covered once we either retire the legacy any-typed Filter input or we
// drop the unused case in v0.2.)

// ----------------------------------------------------------------------------
// Retriever / Indexer / Index lookup wrappers.
// ----------------------------------------------------------------------------

func TestRetrieverIndexer_RegisteredAfterInit(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx,
		genkit.WithPlugins(&Qdrant{
			Configs: []Config{{
				CollectionName: "test_collection",
				ClientParams:   ClientParams{Host: "localhost", Port: 6334},
				Embedder:       stubEmbedder{},
			}},
		}),
	)

	// Bare-name retriever resolves.
	if got := Retriever(g, "test_collection"); got == nil {
		t.Errorf("Retriever(test_collection) = nil, want non-nil")
	}
	// Indexer resolves and reports its registered name.
	idx := Indexer(g, "test_collection")
	if idx == nil {
		t.Fatal("Indexer(test_collection) = nil, want non-nil")
	}
	if got := idx.Name(); got != "test_collection" {
		t.Errorf("Indexer.Name() = %q, want test_collection", got)
	}
}

func TestRetrieverIndexer_UnknownNameReturnsNil(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx,
		genkit.WithPlugins(&Qdrant{
			Configs: []Config{{
				CollectionName: "test_collection",
				ClientParams:   ClientParams{Host: "localhost", Port: 6334},
				Embedder:       stubEmbedder{},
			}},
		}),
	)

	if got := Retriever(g, "does_not_exist"); got != nil {
		t.Errorf("Retriever(unknown) = %v, want nil", got)
	}
	if got := Indexer(g, "does_not_exist"); got != nil {
		t.Errorf("Indexer(unknown) = %v, want nil", got)
	}
}

func TestIndexer_NoQdrantPluginAttachedReturnsNil(t *testing.T) {
	ctx := context.Background()
	// genkit.Init with no plugins → LookupPlugin returns nil.
	g := genkit.Init(ctx)
	if got := Indexer(g, "anything"); got != nil {
		t.Errorf("Indexer with no plugin = %v, want nil", got)
	}
}

func TestIndex_Wrapper_UnknownIndexerReturnsError(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx,
		genkit.WithPlugins(&Qdrant{
			Configs: []Config{{
				CollectionName: "test_collection",
				ClientParams:   ClientParams{Host: "localhost", Port: 6334},
				Embedder:       stubEmbedder{},
			}},
		}),
	)

	err := Index(ctx, g, "missing_collection", []*ai.Document{ai.DocumentFromText("x", nil)})
	if err == nil {
		t.Fatal("Index(unknown) returned nil error, want failure")
	}
	if !strings.Contains(err.Error(), "missing_collection") {
		t.Errorf("error = %q, want it to mention the missing name", err)
	}
}

// ----------------------------------------------------------------------------
// Init panics on bad config and on a second call.
// ----------------------------------------------------------------------------

func TestQdrant_Name(t *testing.T) {
	q := &Qdrant{}
	if got := q.Name(); got != provider {
		t.Errorf("Name() = %q, want %q", got, provider)
	}
}

func TestInit_PanicsOnInvalidConfig(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on invalid Config (missing CollectionName)")
		}
	}()
	q := &Qdrant{Configs: []Config{{Embedder: stubEmbedder{}}}}
	_ = q.Init(context.Background())
}

func TestInit_PanicsOnSecondCall(t *testing.T) {
	q := &Qdrant{Configs: []Config{{
		CollectionName: "test_collection",
		ClientParams:   ClientParams{Host: "localhost", Port: 6334},
		Embedder:       stubEmbedder{},
	}}}
	if actions := q.Init(context.Background()); len(actions) == 0 {
		t.Fatal("first Init produced no actions")
	}
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on second Init")
		}
	}()
	_ = q.Init(context.Background())
}

// ----------------------------------------------------------------------------
// retrieverImpl.retrieve — error paths reachable without a live Qdrant.
// ----------------------------------------------------------------------------

func TestRetrieve_NilRequest(t *testing.T) {
	r := &retrieverImpl{
		cfg: &Config{Embedder: stubEmbedder{}, CollectionName: "test_collection"},
	}
	_, err := r.retrieve(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "nil query") {
		t.Errorf("err=%v, want 'nil query'", err)
	}
}

func TestRetrieve_NilQuery(t *testing.T) {
	r := &retrieverImpl{
		cfg: &Config{Embedder: stubEmbedder{}, CollectionName: "test_collection"},
	}
	_, err := r.retrieve(context.Background(), &ai.RetrieverRequest{Query: nil})
	if err == nil || !strings.Contains(err.Error(), "nil query") {
		t.Errorf("err=%v, want 'nil query'", err)
	}
}

func TestRetrieve_EmbedderError(t *testing.T) {
	wantErr := errors.New("boom")
	r := &retrieverImpl{
		cfg: &Config{Embedder: &failingEmbedder{err: wantErr}, CollectionName: "test_collection"},
	}
	_, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query: ai.DocumentFromText("hi", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "embed query") {
		t.Errorf("err=%v, want wrapping with 'embed query'", err)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is wantErr: %v", err)
	}
}

func TestRetrieve_NoEmbeddings(t *testing.T) {
	r := &retrieverImpl{
		cfg: &Config{Embedder: emptyEmbedder{}, CollectionName: "test_collection"},
	}
	_, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query: ai.DocumentFromText("hi", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "no embeddings") {
		t.Errorf("err=%v, want 'no embeddings'", err)
	}
}

func TestRetrieve_BadFilter(t *testing.T) {
	r := &retrieverImpl{
		cfg: &Config{Embedder: stubEmbedder{}, CollectionName: "test_collection"},
	}
	_, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query:   ai.DocumentFromText("hi", nil),
		Options: &RetrieverOptions{K: 1, Filter: 123 /* unsupported type */},
	})
	if err == nil || !strings.Contains(err.Error(), "filter") {
		t.Errorf("err=%v, want filter error", err)
	}
}

// ----------------------------------------------------------------------------
// indexerImpl.Index — error paths reachable without a live Qdrant.
// ----------------------------------------------------------------------------

func TestIndex_EmptyDocs_NoOp(t *testing.T) {
	i := &indexerImpl{
		cfg: &Config{Embedder: stubEmbedder{}, CollectionName: "test_collection"},
	}
	if err := i.Index(context.Background(), nil); err != nil {
		t.Errorf("Index(nil) returned error: %v", err)
	}
	if err := i.Index(context.Background(), []*ai.Document{}); err != nil {
		t.Errorf("Index(empty) returned error: %v", err)
	}
}

func TestIndex_EmbedderError(t *testing.T) {
	wantErr := errors.New("kaboom")
	i := &indexerImpl{
		cfg: &Config{Embedder: &failingEmbedder{err: wantErr}, CollectionName: "test_collection"},
	}
	err := i.Index(context.Background(), []*ai.Document{ai.DocumentFromText("hi", nil)})
	if err == nil || !strings.Contains(err.Error(), "embed") {
		t.Errorf("err=%v, want wrapping with 'embed'", err)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is wantErr: %v", err)
	}
}

func TestIndex_EmbeddingCountMismatch(t *testing.T) {
	i := &indexerImpl{
		cfg: &Config{Embedder: mismatchEmbedder{}, CollectionName: "test_collection"},
	}
	err := i.Index(context.Background(), []*ai.Document{
		ai.DocumentFromText("a", nil),
		ai.DocumentFromText("b", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "embeddings for") {
		t.Errorf("err=%v, want count-mismatch error", err)
	}
}

func TestIndexer_Name(t *testing.T) {
	i := newIndexer("foo", &Config{}, nil)
	if got := i.Name(); got != "foo" {
		t.Errorf("Name() = %q, want foo", got)
	}
}

func TestNewRetriever_FieldsCopied(t *testing.T) {
	cfg := &Config{
		CollectionName:     "c",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "txt",
		MetadataPayloadKey: "md",
		VectorName:         "vec",
	}
	r := newRetriever("c/vec", cfg, nil)
	if r.name != "c/vec" || r.contentKey != "txt" || r.metaKey != "md" || r.vectorName != "vec" {
		t.Errorf("retrieverImpl fields not copied as expected: %+v", r)
	}
}

func TestNewIndexer_FieldsCopied(t *testing.T) {
	cfg := &Config{
		CollectionName:     "c",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "txt",
		MetadataPayloadKey: "md",
		VectorName:         "vec",
	}
	i := newIndexer("c/vec", cfg, nil)
	if i.name != "c/vec" || i.contentKey != "txt" || i.metaKey != "md" || i.vectorName != "vec" {
		t.Errorf("indexerImpl fields not copied as expected: %+v", i)
	}
}
