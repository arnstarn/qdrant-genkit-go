// Package qdrant provides a Firebase Genkit Go plugin for the Qdrant vector
// database. It exposes Indexer and Retriever implementations that integrate
// with any Genkit Embedder.
//
// Mirror of the JS/TS @genkit-ai/qdrant plugin's API where reasonable. Genkit
// Go 1.x does not yet have a first-class Indexer abstraction; this package
// fills that gap with a minimal Indexer interface and a package-level Index
// helper, kept deliberately close to how plugins like pinecone and localvec
// expose indexing in genkit-go.
package qdrant

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/core/api"
	"github.com/firebase/genkit/go/genkit"
	qclient "github.com/qdrant/go-client/qdrant"

	"github.com/arnstarn/qdrant-genkit-go/internal/client"
	"github.com/arnstarn/qdrant-genkit-go/internal/convert"
)

// provider is the registry namespace used for all retrievers/indexers
// registered by this plugin.
const provider = "qdrant"

// Default payload keys used when a Config does not specify its own.
const (
	DefaultContentPayloadKey  = "content"
	DefaultMetadataPayloadKey = "metadata"
)

// Qdrant is the Genkit plugin entry point. Pass it to genkit.WithPlugins.
type Qdrant struct {
	// Configs declares one or more (collection, vector) configurations.
	// For named-vector collections, declare one Config per vector slot you
	// want to expose as a retriever/indexer.
	Configs []Config

	mu       sync.Mutex
	clients  map[string]*qclient.Client // keyed by host:port:apikey
	indexers map[string]*indexerImpl    // keyed by registered name
	initted  bool
}

// Config configures a single retriever/indexer pair targeting one Qdrant
// collection (and optionally one named vector slot inside it).
type Config struct {
	// CollectionName is the Qdrant collection name. Required.
	CollectionName string

	// ClientParams configures the connection to Qdrant.
	ClientParams ClientParams

	// Embedder is the Genkit embedder used for both indexing and retrieval
	// queries. Required.
	Embedder ai.Embedder

	// ContentPayloadKey is the payload key holding document text.
	// Default: "content".
	ContentPayloadKey string

	// MetadataPayloadKey is the payload key holding metadata.
	// Default: "metadata".
	MetadataPayloadKey string

	// VectorName is the name of the vector slot in named-vector collections.
	// Leave empty for single-vector collections.
	VectorName string
}

// ClientParams describes how to reach a Qdrant instance.
//
// Note: the official Qdrant Go client uses gRPC. Qdrant exposes gRPC on port
// 6334 and REST on port 6333; this plugin defaults Port to 6334 when zero.
type ClientParams struct {
	Host   string // default: "localhost"
	Port   int    // default: 6334 (gRPC). Qdrant's REST port is 6333.
	APIKey string // optional
	UseTLS bool   // default: false
}

// RetrieverOptions are passed via Genkit's RetrieverRequest.Options to control
// a single retrieval call. Use ai.WithConfig(&qdrant.RetrieverOptions{...}) at
// the call site, or set the options on a RetrieverRef.
type RetrieverOptions struct {
	// K is the maximum number of documents to return. Default: 3.
	K int

	// Filter is an optional Qdrant payload filter. Two forms are accepted:
	//   - map[string]any: convenient JSON-like form; supports "must",
	//     "should", "must_not" with simple match/range conditions. See the
	//     README for examples and supported subset.
	//   - *qdrant.Filter (from github.com/qdrant/go-client/qdrant): pass
	//     through unchanged for arbitrary filters (geo, datetime, nested).
	//
	// Note: pass *qdrant.Filter (a pointer), not a value. The underlying
	// generated protobuf type embeds a sync.Mutex via its message machinery,
	// so passing it by value would trip `go vet copylocks`.
	Filter any

	// ScoreThreshold, if set, drops results whose similarity score is below
	// this value.
	ScoreThreshold *float32
}

// IndexerHandle is the public method set of an indexer registered by this
// plugin. Genkit Go 1.x does not yet expose an Indexer interface in the ai
// package, so we define our own minimal one.
//
// Once Genkit Go gains an indexer abstraction we plan to align the indexer's
// shape to it; this interface should remain a strict subset.
type IndexerHandle interface {
	// Name returns the indexer's registered name.
	Name() string
	// Index embeds and upserts the given documents into the underlying
	// Qdrant collection.
	Index(ctx context.Context, docs []*ai.Document) error
}

// Name returns the plugin name as required by the Genkit plugin interface.
func (q *Qdrant) Name() string { return provider }

// Init initializes the plugin: it validates each Config, opens a Qdrant client
// per unique connection, and registers a Retriever (and prepares an Indexer)
// for every Config.
//
// This signature satisfies the genkit core/api.Plugin interface
// (Init(ctx) []api.Action). Returned Actions are the plugin's retrievers; they
// are registered with the Genkit registry by genkit.Init.
func (q *Qdrant) Init(ctx context.Context) []api.Action {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.initted {
		// Match other plugins (e.g., pinecone): a second Init is a bug.
		panic("qdrant.Init already called")
	}

	if q.clients == nil {
		q.clients = make(map[string]*qclient.Client)
	}
	if q.indexers == nil {
		q.indexers = make(map[string]*indexerImpl)
	}

	actions := make([]api.Action, 0, len(q.Configs))
	for i := range q.Configs {
		cfg := &q.Configs[i]
		if err := validateConfig(cfg); err != nil {
			panic(fmt.Errorf("qdrant.Init: configs[%d]: %w", i, err))
		}
		applyConfigDefaults(cfg)

		c, err := q.clientFor(cfg.ClientParams)
		if err != nil {
			panic(fmt.Errorf("qdrant.Init: configs[%d]: %w", i, err))
		}

		name := registerName(cfg.CollectionName, cfg.VectorName)
		retr := newRetriever(name, cfg, c)
		idx := newIndexer(name, cfg, c)
		q.indexers[name] = idx

		// ai.NewRetriever returns an action.Registerable that we hand back
		// to genkit.Init for registration.
		retrAction := ai.NewRetriever(api.NewName(provider, name), nil, retr.retrieve)
		// ai.Retriever satisfies api.Action via embedded ActionDef. Cast.
		if a, ok := retrAction.(api.Action); ok {
			actions = append(actions, a)
		} else {
			panic(fmt.Errorf("qdrant.Init: ai.NewRetriever returned %T, want api.Action", retrAction))
		}
	}

	q.initted = true
	return actions
}

// validateConfig checks that the required fields are set.
func validateConfig(cfg *Config) error {
	if cfg.CollectionName == "" {
		return errors.New("CollectionName is required")
	}
	if cfg.Embedder == nil {
		return errors.New("Embedder is required")
	}
	return nil
}

// applyConfigDefaults fills in unset fields with their documented defaults.
func applyConfigDefaults(cfg *Config) {
	if cfg.ContentPayloadKey == "" {
		cfg.ContentPayloadKey = DefaultContentPayloadKey
	}
	if cfg.MetadataPayloadKey == "" {
		cfg.MetadataPayloadKey = DefaultMetadataPayloadKey
	}
}

// newClientFn is the constructor used by clientFor. It exists as a
// package-level variable so tests can substitute a stub that simulates a
// connection failure. Production code always uses client.New.
var newClientFn = client.New

// clientFor returns a Qdrant client for the given connection params, creating
// it on first use and reusing it across configs that share an endpoint.
func (q *Qdrant) clientFor(cp ClientParams) (*qclient.Client, error) {
	key := fmt.Sprintf("%s:%d:%s:%t", cp.Host, cp.Port, cp.APIKey, cp.UseTLS)
	if c, ok := q.clients[key]; ok {
		return c, nil
	}
	c, err := newClientFn(client.Params{
		Host:   cp.Host,
		Port:   cp.Port,
		APIKey: cp.APIKey,
		UseTLS: cp.UseTLS,
	})
	if err != nil {
		return nil, err
	}
	q.clients[key] = c
	return c, nil
}

// registerName builds the registry name for a Config. Single-vector configs
// register at the bare collection name; named-vector configs at
// "collection/vector".
func registerName(collection, vectorName string) string {
	if vectorName == "" {
		return collection
	}
	return collection + "/" + vectorName
}

// Retriever returns the registered retriever for the given collection name.
// For named-vector collections, use the form "collection/vector_name".
//
// Returns nil if no retriever was registered with that name.
func Retriever(g *genkit.Genkit, name string) ai.Retriever {
	return genkit.LookupRetriever(g, api.NewName(provider, name))
}

// Indexer returns the registered indexer for the given collection name. For
// named-vector collections, use the form "collection/vector_name".
//
// Returns nil if no indexer was registered with that name. Indexers are looked
// up via the Qdrant plugin instance attached to g.
func Indexer(g *genkit.Genkit, name string) IndexerHandle {
	p := genkit.LookupPlugin(g, provider)
	if p == nil {
		return nil
	}
	q, ok := p.(*Qdrant)
	if !ok {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	idx, ok := q.indexers[name]
	if !ok {
		return nil
	}
	return idx
}

// Index is a convenience wrapper that invokes the named indexer with the
// provided documents. It is equivalent to looking up the indexer and calling
// its Index method, and exists to keep the public API symmetric with the
// retriever side (ai.Retrieve).
func Index(ctx context.Context, g *genkit.Genkit, name string, docs []*ai.Document) error {
	idx := Indexer(g, name)
	if idx == nil {
		return fmt.Errorf("qdrant: indexer %q not found", name)
	}
	return idx.Index(ctx, docs)
}

// pointsAPI is the subset of *qclient.Client used by retrieverImpl and
// indexerImpl. Defining it as an interface lets unit tests substitute a fake
// without spinning up Qdrant; the production code path always passes the
// concrete *qclient.Client, which satisfies it via embedded methods.
type pointsAPI interface {
	Query(ctx context.Context, request *qclient.QueryPoints) ([]*qclient.ScoredPoint, error)
	Upsert(ctx context.Context, request *qclient.UpsertPoints) (*qclient.UpdateResult, error)
}

// retrieverImpl bundles the per-config state needed to serve a retrieval
// request: the Qdrant client, the collection/vector to target, and the
// embedder to vectorize the query.
type retrieverImpl struct {
	name       string
	cfg        *Config
	c          pointsAPI
	contentKey string
	metaKey    string
	vectorName string
}

func newRetriever(name string, cfg *Config, c pointsAPI) *retrieverImpl {
	return &retrieverImpl{
		name:       name,
		cfg:        cfg,
		c:          c,
		contentKey: cfg.ContentPayloadKey,
		metaKey:    cfg.MetadataPayloadKey,
		vectorName: cfg.VectorName,
	}
}

// retrieve is the RetrieverFunc registered with Genkit. It embeds the query,
// runs a Qdrant search, and converts the resulting points back into
// ai.Documents.
func (r *retrieverImpl) retrieve(ctx context.Context, req *ai.RetrieverRequest) (*ai.RetrieverResponse, error) {
	if req == nil || req.Query == nil {
		return nil, errors.New("qdrant.retrieve: nil query")
	}

	opts := optsFromRequest(req)

	// Embed the query document.
	er, err := r.cfg.Embedder.Embed(ctx, &ai.EmbedRequest{Input: []*ai.Document{req.Query}})
	if err != nil {
		return nil, fmt.Errorf("qdrant.retrieve: embed query: %w", err)
	}
	if len(er.Embeddings) == 0 {
		return nil, errors.New("qdrant.retrieve: embedder returned no embeddings")
	}
	vec := er.Embeddings[0].Embedding

	// Build the Qdrant query.
	limit := uint64(opts.K)
	q := &qclient.QueryPoints{
		CollectionName: r.cfg.CollectionName,
		Query:          qclient.NewQuery(vec...),
		Limit:          &limit,
		WithPayload:    qclient.NewWithPayloadEnable(true),
	}
	if r.vectorName != "" {
		vn := r.vectorName
		q.Using = &vn
	}
	if opts.ScoreThreshold != nil {
		q.ScoreThreshold = opts.ScoreThreshold
	}
	if opts.Filter != nil {
		f, err := buildFilter(opts.Filter)
		if err != nil {
			return nil, fmt.Errorf("qdrant.retrieve: filter: %w", err)
		}
		q.Filter = f
	}

	// Execute.
	hits, err := r.c.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("qdrant.retrieve: query: %w", err)
	}

	// Convert hits → documents.
	docs := make([]*ai.Document, 0, len(hits))
	for _, h := range hits {
		docs = append(docs, convert.ScoredPointToDocument(h, r.contentKey, r.metaKey))
	}
	return &ai.RetrieverResponse{Documents: docs}, nil
}

// optsFromRequest extracts our RetrieverOptions from a Genkit RetrieverRequest.
// The request's Options field can be nil, *RetrieverOptions, or RetrieverOptions
// (by value).
func optsFromRequest(req *ai.RetrieverRequest) RetrieverOptions {
	out := RetrieverOptions{K: 3}
	switch v := req.Options.(type) {
	case nil:
		// keep defaults
	case *RetrieverOptions:
		if v != nil {
			out = *v
		}
	case RetrieverOptions:
		out = v
	}
	if out.K <= 0 {
		out.K = 3
	}
	return out
}

// buildFilter accepts either a *qclient.Filter (passed through) or a
// map[string]any that we translate via the convert package.
//
// Note: only the pointer form is accepted for *qclient.Filter; passing a
// qclient.Filter by value trips `go vet copylocks` because the generated
// protobuf message machinery embeds a sync.Mutex.
func buildFilter(filter any) (*qclient.Filter, error) {
	switch f := filter.(type) {
	case nil:
		return nil, nil
	case *qclient.Filter:
		return f, nil
	case map[string]any:
		return convert.FilterFromMap(f)
	default:
		return nil, fmt.Errorf("unsupported filter type %T; expected map[string]any or *qdrant.Filter", filter)
	}
}

// indexerImpl is the concrete Indexer registered for each Config.
type indexerImpl struct {
	name       string
	cfg        *Config
	c          pointsAPI
	contentKey string
	metaKey    string
	vectorName string
}

func newIndexer(name string, cfg *Config, c pointsAPI) *indexerImpl {
	return &indexerImpl{
		name:       name,
		cfg:        cfg,
		c:          c,
		contentKey: cfg.ContentPayloadKey,
		metaKey:    cfg.MetadataPayloadKey,
		vectorName: cfg.VectorName,
	}
}

func (i *indexerImpl) Name() string { return i.name }

func (i *indexerImpl) Index(ctx context.Context, docs []*ai.Document) error {
	if len(docs) == 0 {
		return nil
	}

	// Embed all docs in one call. (For very large batches a caller should
	// pre-chunk; the embedder controls its own batch limits.)
	er, err := i.cfg.Embedder.Embed(ctx, &ai.EmbedRequest{Input: docs})
	if err != nil {
		return fmt.Errorf("qdrant.index: embed: %w", err)
	}
	if len(er.Embeddings) != len(docs) {
		return fmt.Errorf("qdrant.index: embedder returned %d embeddings for %d docs",
			len(er.Embeddings), len(docs))
	}

	points := make([]*qclient.PointStruct, 0, len(docs))
	for n, d := range docs {
		p, err := convert.DocumentToPoint(d, er.Embeddings[n].Embedding, i.vectorName, i.contentKey, i.metaKey)
		if err != nil {
			return fmt.Errorf("qdrant.index: build point %d: %w", n, err)
		}
		points = append(points, p)
	}

	wait := true
	_, err = i.c.Upsert(ctx, &qclient.UpsertPoints{
		CollectionName: i.cfg.CollectionName,
		Wait:           &wait,
		Points:         points,
	})
	if err != nil {
		return fmt.Errorf("qdrant.index: upsert: %w", err)
	}
	return nil
}
