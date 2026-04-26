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

	"github.com/arnstarn/qdrant-genkit-go/internal/client"
)

// failingEmbedder is an embedder that always returns the configured error so
// tests can hit the error branches in retrieve / Index without a real model.
type failingEmbedder struct {
	err error
}

func (f *failingEmbedder) Name() string          { return "fail" }
func (f *failingEmbedder) Register(api.Registry) {} //nolint:revive
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
// buildFilter — pointer-only contract.
// ----------------------------------------------------------------------------
//
// The pointer-passthrough branch is exercised in TestBuildFilter_Passthrough.
// We deliberately do not accept qclient.Filter by value: the generated
// protobuf type embeds a sync.Mutex, so a by-value branch would trip `go vet
// copylocks` for any test attempting to exercise it. Callers should pass
// *qdrant.Filter; the by-value branch was removed.

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

// ----------------------------------------------------------------------------
// fakePoints satisfies the pointsAPI interface so unit tests can drive the
// retrieve and Index code paths past the gRPC call without a live Qdrant.
// ----------------------------------------------------------------------------

type fakePoints struct {
	// Captured by Query/Upsert.
	queryReq  *qclient.QueryPoints
	upsertReq *qclient.UpsertPoints

	// Outputs.
	queryHits  []*qclient.ScoredPoint
	queryErr   error
	upsertResp *qclient.UpdateResult
	upsertErr  error
}

func (f *fakePoints) Query(_ context.Context, req *qclient.QueryPoints) ([]*qclient.ScoredPoint, error) {
	f.queryReq = req
	return f.queryHits, f.queryErr
}

func (f *fakePoints) Upsert(_ context.Context, req *qclient.UpsertPoints) (*qclient.UpdateResult, error) {
	f.upsertReq = req
	return f.upsertResp, f.upsertErr
}

// ----------------------------------------------------------------------------
// retrieverImpl.retrieve — happy path + post-Query error wrapping. These
// exercise the post-embedding code paths that previously required a live
// Qdrant container, including filter application, named-vector slot wiring,
// score-threshold passthrough, and the hit→Document conversion loop.
// ----------------------------------------------------------------------------

func TestRetrieve_HappyPath(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	hit := &qclient.ScoredPoint{
		Payload: qclient.NewValueMap(map[string]any{
			"content":  "hello",
			"metadata": map[string]any{"src": "test"},
		}),
		Score: 0.42,
	}
	fp := &fakePoints{queryHits: []*qclient.ScoredPoint{hit}}
	r := newRetriever("test_collection", cfg, fp)

	resp, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query:   ai.DocumentFromText("query", nil),
		Options: &RetrieverOptions{K: 5},
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(resp.Documents) != 1 {
		t.Fatalf("got %d docs, want 1", len(resp.Documents))
	}
	if got := resp.Documents[0].Content[0].Text; got != "hello" {
		t.Errorf("doc text = %q, want hello", got)
	}
	// Limit was forwarded.
	if fp.queryReq == nil || fp.queryReq.Limit == nil || *fp.queryReq.Limit != 5 {
		t.Errorf("Limit = %v, want 5", fp.queryReq.Limit)
	}
	// Bare collection: no Using slot set.
	if fp.queryReq.Using != nil {
		t.Errorf("Using = %v, want nil for single-vector collection", fp.queryReq.Using)
	}
	// CollectionName is propagated.
	if fp.queryReq.CollectionName != "test_collection" {
		t.Errorf("CollectionName = %q, want test_collection", fp.queryReq.CollectionName)
	}
}

func TestRetrieve_NamedVectorSetsUsing(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
		VectorName:         "text",
	}
	fp := &fakePoints{}
	r := newRetriever("test_collection/text", cfg, fp)
	if _, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query: ai.DocumentFromText("q", nil),
	}); err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if fp.queryReq.Using == nil || *fp.queryReq.Using != "text" {
		t.Errorf("Using = %v, want 'text'", fp.queryReq.Using)
	}
}

func TestRetrieve_ScoreThresholdForwarded(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	fp := &fakePoints{}
	r := newRetriever("test_collection", cfg, fp)
	thr := float32(0.6)
	if _, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query:   ai.DocumentFromText("q", nil),
		Options: &RetrieverOptions{ScoreThreshold: &thr},
	}); err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if fp.queryReq.ScoreThreshold == nil || *fp.queryReq.ScoreThreshold != 0.6 {
		t.Errorf("ScoreThreshold = %v, want 0.6", fp.queryReq.ScoreThreshold)
	}
}

func TestRetrieve_FilterMapForwarded(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	fp := &fakePoints{}
	r := newRetriever("test_collection", cfg, fp)
	if _, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query: ai.DocumentFromText("q", nil),
		Options: &RetrieverOptions{
			Filter: map[string]any{
				"must": []map[string]any{
					{"key": "src", "match": map[string]any{"value": "test"}},
				},
			},
		},
	}); err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if fp.queryReq.Filter == nil || len(fp.queryReq.Filter.Must) != 1 {
		t.Errorf("Filter not forwarded; got %v", fp.queryReq.Filter)
	}
}

func TestRetrieve_QueryError(t *testing.T) {
	wantErr := errors.New("rpc boom")
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	fp := &fakePoints{queryErr: wantErr}
	r := newRetriever("test_collection", cfg, fp)
	_, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query: ai.DocumentFromText("q", nil),
	})
	if err == nil || !strings.Contains(err.Error(), "qdrant.retrieve: query") {
		t.Errorf("err = %v, want wrapping with 'qdrant.retrieve: query'", err)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is wantErr: %v", err)
	}
}

func TestRetrieve_EmptyHits(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	fp := &fakePoints{queryHits: nil}
	r := newRetriever("test_collection", cfg, fp)
	resp, err := r.retrieve(context.Background(), &ai.RetrieverRequest{
		Query: ai.DocumentFromText("q", nil),
	})
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if len(resp.Documents) != 0 {
		t.Errorf("got %d docs, want 0", len(resp.Documents))
	}
}

// ----------------------------------------------------------------------------
// indexerImpl.Index — happy path + Upsert error wrapping. These cover the
// document→point conversion loop, the wait flag, and the post-Upsert error
// wrap. The pre-call error paths (empty docs, embedder failure, count
// mismatch) are covered above.
// ----------------------------------------------------------------------------

func TestIndex_HappyPath(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	fp := &fakePoints{}
	i := newIndexer("test_collection", cfg, fp)

	docs := []*ai.Document{
		ai.DocumentFromText("alpha", map[string]any{"i": 1}),
		ai.DocumentFromText("beta", map[string]any{"i": 2}),
	}
	if err := i.Index(context.Background(), docs); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if fp.upsertReq == nil {
		t.Fatal("Upsert was not invoked")
	}
	if fp.upsertReq.CollectionName != "test_collection" {
		t.Errorf("CollectionName = %q, want test_collection", fp.upsertReq.CollectionName)
	}
	if len(fp.upsertReq.Points) != len(docs) {
		t.Errorf("len(Points) = %d, want %d", len(fp.upsertReq.Points), len(docs))
	}
	if fp.upsertReq.Wait == nil || !*fp.upsertReq.Wait {
		t.Errorf("Wait = %v, want true", fp.upsertReq.Wait)
	}
}

func TestIndex_NamedVectorPath(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
		VectorName:         "text",
	}
	fp := &fakePoints{}
	i := newIndexer("test_collection/text", cfg, fp)
	if err := i.Index(context.Background(), []*ai.Document{
		ai.DocumentFromText("only", nil),
	}); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if len(fp.upsertReq.Points) != 1 {
		t.Fatalf("len(Points) = %d, want 1", len(fp.upsertReq.Points))
	}
	// Named vector → Vectors oneof should be the named-map form.
	v := fp.upsertReq.Points[0].Vectors
	if _, ok := v.GetVectorsOptions().(*qclient.Vectors_Vectors); !ok {
		t.Errorf("expected named-vector wrapping, got %T", v.GetVectorsOptions())
	}
}

func TestIndex_UpsertError(t *testing.T) {
	wantErr := errors.New("upsert boom")
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	fp := &fakePoints{upsertErr: wantErr}
	i := newIndexer("test_collection", cfg, fp)
	err := i.Index(context.Background(), []*ai.Document{ai.DocumentFromText("x", nil)})
	if err == nil || !strings.Contains(err.Error(), "qdrant.index: upsert") {
		t.Errorf("err = %v, want wrapping with 'qdrant.index: upsert'", err)
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is wantErr: %v", err)
	}
}

// ----------------------------------------------------------------------------
// Index wrapper — happy path: when the registered indexer succeeds, the
// package-level Index returns nil. Backed by a stub indexer that we install
// directly into the plugin's internal map.
// ----------------------------------------------------------------------------

func TestIndex_Wrapper_Success(t *testing.T) {
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

	// Replace the indexer's pointsAPI with a fake to avoid the gRPC hop.
	idx := Indexer(g, "test_collection")
	if idx == nil {
		t.Fatal("indexer not registered")
	}
	impl, ok := idx.(*indexerImpl)
	if !ok {
		t.Fatalf("indexer is %T, want *indexerImpl", idx)
	}
	impl.c = &fakePoints{}

	if err := Index(ctx, g, "test_collection", []*ai.Document{
		ai.DocumentFromText("payload", nil),
	}); err != nil {
		t.Errorf("Index wrapper returned %v, want nil", err)
	}
}

// ----------------------------------------------------------------------------
// Indexer plugin-cast guard: Indexer() must return nil when the plugin
// registered under "qdrant" is not actually a *Qdrant. We force this by
// registering a stub plugin.
// ----------------------------------------------------------------------------

type bogusPlugin struct{}

func (bogusPlugin) Name() string                        { return provider }
func (bogusPlugin) Init(_ context.Context) []api.Action { return nil }

func TestIndexer_PluginCastFails_ReturnsNil(t *testing.T) {
	ctx := context.Background()
	g := genkit.Init(ctx, genkit.WithPlugins(bogusPlugin{}))
	if got := Indexer(g, "anything"); got != nil {
		t.Errorf("Indexer with non-Qdrant plugin = %v, want nil", got)
	}
}

// ----------------------------------------------------------------------------
// Index — convert.DocumentToPoint error path. A nil document survives the
// embedder count check (the stub allocates one slot per input) but trips the
// "convert: nil document" branch in DocumentToPoint, which we expect to wrap
// with "build point".
// ----------------------------------------------------------------------------

func TestIndex_BuildPointError(t *testing.T) {
	cfg := &Config{
		CollectionName:     "test_collection",
		Embedder:           stubEmbedder{},
		ContentPayloadKey:  "content",
		MetadataPayloadKey: "metadata",
	}
	fp := &fakePoints{}
	i := newIndexer("test_collection", cfg, fp)

	err := i.Index(context.Background(), []*ai.Document{nil})
	if err == nil || !strings.Contains(err.Error(), "build point") {
		t.Errorf("err = %v, want wrapping with 'build point'", err)
	}
	// We never reached Upsert.
	if fp.upsertReq != nil {
		t.Errorf("Upsert should not be called when point conversion fails")
	}
}

// ----------------------------------------------------------------------------
// clientFor / Init — connection failures. We swap newClientFn to force an
// error and assert (a) clientFor returns it unchanged, and (b) Init wraps it
// with the configs[i] prefix and panics.
// ----------------------------------------------------------------------------

func withFailingNewClient(t *testing.T, err error) {
	t.Helper()
	prev := newClientFn
	newClientFn = func(client.Params) (*qclient.Client, error) {
		return nil, err
	}
	t.Cleanup(func() { newClientFn = prev })
}

func TestClientFor_NewClientError(t *testing.T) {
	wantErr := errors.New("connection refused")
	withFailingNewClient(t, wantErr)

	q := newQdrantWithMaps()
	_, err := q.clientFor(ClientParams{Host: "x", Port: 1})
	if err == nil {
		t.Fatal("expected error")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("errors.Is wantErr: %v", err)
	}
	if len(q.clients) != 0 {
		t.Errorf("clients map has %d entries, want 0 on failure", len(q.clients))
	}
}

func TestInit_PanicsOnClientForFailure(t *testing.T) {
	wantErr := errors.New("dial busted")
	withFailingNewClient(t, wantErr)

	q := &Qdrant{Configs: []Config{{
		CollectionName: "test_collection",
		Embedder:       stubEmbedder{},
	}}}

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("recovered %T, want error", r)
		}
		if !strings.Contains(err.Error(), "configs[0]") {
			t.Errorf("err = %v, want it to mention configs[0]", err)
		}
		if !errors.Is(err, wantErr) {
			t.Errorf("errors.Is wantErr: %v", err)
		}
	}()
	_ = q.Init(context.Background())
}
