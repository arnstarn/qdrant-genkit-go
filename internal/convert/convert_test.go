package convert

import (
	"strings"
	"testing"

	"github.com/firebase/genkit/go/ai"
	qclient "github.com/qdrant/go-client/qdrant"
)

func TestDocumentText_Basic(t *testing.T) {
	doc := ai.DocumentFromText("hello world", nil)
	if got := DocumentText(doc); got != "hello world" {
		t.Errorf("DocumentText = %q, want %q", got, "hello world")
	}
}

func TestDocumentText_MultiPart(t *testing.T) {
	doc := &ai.Document{
		Content: []*ai.Part{
			ai.NewTextPart("foo "),
			ai.NewTextPart("bar"),
		},
	}
	if got := DocumentText(doc); got != "foo bar" {
		t.Errorf("DocumentText = %q, want %q", got, "foo bar")
	}
}

func TestDocumentText_Nil(t *testing.T) {
	if got := DocumentText(nil); got != "" {
		t.Errorf("DocumentText(nil) = %q, want empty string", got)
	}
}

func TestDocumentID_Stable(t *testing.T) {
	doc := ai.DocumentFromText("repeatable input", map[string]any{"k": "v"})
	id1, err := DocumentID(doc)
	if err != nil {
		t.Fatalf("DocumentID: %v", err)
	}
	id2, err := DocumentID(doc)
	if err != nil {
		t.Fatalf("DocumentID: %v", err)
	}
	if id1 != id2 {
		t.Errorf("DocumentID not stable: %q vs %q", id1, id2)
	}
	// UUID-shaped: 8-4-4-4-12 with dashes.
	if len(id1) != 36 {
		t.Errorf("DocumentID length = %d, want 36 (uuid)", len(id1))
	}
}

func TestDocumentID_Distinct(t *testing.T) {
	id1, _ := DocumentID(ai.DocumentFromText("alpha", nil))
	id2, _ := DocumentID(ai.DocumentFromText("beta", nil))
	if id1 == id2 {
		t.Errorf("expected distinct IDs for distinct documents, got %q twice", id1)
	}
}

func TestValueToAny_Scalars(t *testing.T) {
	tests := []struct {
		name string
		in   *qclient.Value
		want any
	}{
		{"nil", nil, nil},
		{"null", qclient.NewValueNull(), nil},
		{"bool", qclient.NewValueBool(true), true},
		{"int", qclient.NewValueInt(42), int64(42)},
		{"double", qclient.NewValueDouble(3.14), 3.14},
		{"string", qclient.NewValueString("hi"), "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValueToAny(tt.in); got != tt.want {
				t.Errorf("ValueToAny = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestValueToAny_Struct(t *testing.T) {
	v, err := qclient.NewValue(map[string]any{"k": "v", "n": 1})
	if err != nil {
		t.Fatalf("NewValue: %v", err)
	}
	got, ok := ValueToAny(v).(map[string]any)
	if !ok {
		t.Fatalf("ValueToAny did not return map[string]any, got %T", ValueToAny(v))
	}
	if got["k"] != "v" {
		t.Errorf("got[k] = %v, want v", got["k"])
	}
	if got["n"] != int64(1) {
		t.Errorf("got[n] = %v (%T), want int64(1)", got["n"], got["n"])
	}
}

func TestValueToAny_List(t *testing.T) {
	v, err := qclient.NewValue([]any{"a", int64(2), true})
	if err != nil {
		t.Fatalf("NewValue: %v", err)
	}
	got, ok := ValueToAny(v).([]any)
	if !ok {
		t.Fatalf("ValueToAny did not return []any, got %T", ValueToAny(v))
	}
	if len(got) != 3 {
		t.Fatalf("len(got) = %d, want 3", len(got))
	}
	if got[0] != "a" || got[1] != int64(2) || got[2] != true {
		t.Errorf("got = %v, want [a 2 true]", got)
	}
}

func TestPayloadToMap(t *testing.T) {
	in := qclient.NewValueMap(map[string]any{"a": "b", "n": 7})
	got := PayloadToMap(in)
	if got["a"] != "b" {
		t.Errorf("got[a] = %v, want b", got["a"])
	}
	if got["n"] != int64(7) {
		t.Errorf("got[n] = %v, want int64(7)", got["n"])
	}
}

func TestPayloadToMap_Nil(t *testing.T) {
	if got := PayloadToMap(nil); got != nil {
		t.Errorf("PayloadToMap(nil) = %v, want nil", got)
	}
}

func TestScoredPointToDocument_NestedMetadata(t *testing.T) {
	payload := qclient.NewValueMap(map[string]any{
		"content":  "doc text",
		"metadata": map[string]any{"lang": "go", "year": 2024},
	})
	sp := &qclient.ScoredPoint{
		Payload: payload,
		Score:   0.85,
	}

	doc := ScoredPointToDocument(sp, "content", "metadata")
	if doc == nil {
		t.Fatal("ScoredPointToDocument returned nil")
	}
	if got := doc.Content[0].Text; got != "doc text" {
		t.Errorf("text = %q, want %q", got, "doc text")
	}
	if got := doc.Metadata["lang"]; got != "go" {
		t.Errorf("metadata.lang = %v, want go", got)
	}
	if got := doc.Metadata["year"]; got != int64(2024) {
		t.Errorf("metadata.year = %v, want 2024", got)
	}
	if got := doc.Metadata["_score"]; got != float32(0.85) {
		t.Errorf("metadata._score = %v, want 0.85", got)
	}
}

func TestScoredPointToDocument_FlatPayload(t *testing.T) {
	// No nested "metadata" key → flat payload (minus content) becomes metadata.
	payload := qclient.NewValueMap(map[string]any{
		"content": "doc text",
		"source":  "file.md",
	})
	sp := &qclient.ScoredPoint{
		Payload: payload,
		Score:   0.5,
	}

	doc := ScoredPointToDocument(sp, "content", "metadata")
	if doc == nil {
		t.Fatal("ScoredPointToDocument returned nil")
	}
	if got := doc.Content[0].Text; got != "doc text" {
		t.Errorf("text = %q, want %q", got, "doc text")
	}
	if got := doc.Metadata["source"]; got != "file.md" {
		t.Errorf("metadata.source = %v, want file.md", got)
	}
	if _, ok := doc.Metadata["content"]; ok {
		t.Errorf("metadata should not contain the content key")
	}
}

func TestScoredPointToDocument_Nil(t *testing.T) {
	if got := ScoredPointToDocument(nil, "c", "m"); got != nil {
		t.Errorf("ScoredPointToDocument(nil) = %v, want nil", got)
	}
}

func TestDocumentToPoint_Single(t *testing.T) {
	doc := ai.DocumentFromText("hello", map[string]any{"src": "test"})
	p, err := DocumentToPoint(doc, []float32{0.1, 0.2, 0.3}, "", "content", "metadata")
	if err != nil {
		t.Fatalf("DocumentToPoint: %v", err)
	}
	if p.Id == nil {
		t.Fatal("expected an id")
	}
	if p.Vectors == nil {
		t.Fatal("expected vectors")
	}
	// Single-vector path: the Vectors oneof should be a *Vectors_Vector.
	if _, ok := p.Vectors.GetVectorsOptions().(*qclient.Vectors_Vector); !ok {
		t.Errorf("vectors not a single vector, got %T", p.Vectors.GetVectorsOptions())
	}
	// Payload contains content and metadata.
	if got := p.Payload["content"].GetStringValue(); got != "hello" {
		t.Errorf("payload.content = %q, want hello", got)
	}
	if p.Payload["metadata"] == nil {
		t.Errorf("payload.metadata missing")
	}
}

func TestDocumentToPoint_NamedVector(t *testing.T) {
	doc := ai.DocumentFromText("hello", nil)
	p, err := DocumentToPoint(doc, []float32{0.1, 0.2}, "text", "content", "metadata")
	if err != nil {
		t.Fatalf("DocumentToPoint: %v", err)
	}
	// Named-vector path: oneof should be *Vectors_Vectors (the map form).
	v, ok := p.Vectors.GetVectorsOptions().(*qclient.Vectors_Vectors)
	if !ok {
		t.Fatalf("vectors oneof = %T, want *Vectors_Vectors", p.Vectors.GetVectorsOptions())
	}
	if _, ok := v.Vectors.GetVectors()["text"]; !ok {
		t.Errorf("named vector slot 'text' missing; got keys %v", keys(v.Vectors.GetVectors()))
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFilterFromMap_Empty(t *testing.T) {
	f, err := FilterFromMap(nil)
	if err != nil {
		t.Fatalf("FilterFromMap(nil): %v", err)
	}
	if f != nil {
		t.Errorf("expected nil filter for nil input")
	}
	f, err = FilterFromMap(map[string]any{})
	if err != nil {
		t.Fatalf("FilterFromMap(empty): %v", err)
	}
	if f != nil {
		t.Errorf("expected nil filter for empty input")
	}
}

func TestFilterFromMap_MustMatchKeyword(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "lang", "match": map[string]any{"value": "go"}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if f == nil || len(f.Must) != 1 {
		t.Fatalf("Must has %d, want 1", len(f.GetMust()))
	}
	c := f.Must[0].GetField()
	if c == nil {
		t.Fatal("expected field condition")
	}
	if c.Key != "lang" {
		t.Errorf("key = %q, want lang", c.Key)
	}
	if c.Match.GetKeyword() != "go" {
		t.Errorf("match keyword = %q, want go", c.Match.GetKeyword())
	}
}

func TestFilterFromMap_MustRangeGTE(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "year", "range": map[string]any{"gte": 2024}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	c := f.Must[0].GetField()
	if c == nil || c.Range == nil {
		t.Fatal("expected range condition")
	}
	if c.Range.Gte == nil || *c.Range.Gte != 2024 {
		t.Errorf("range.gte = %v, want 2024", c.Range.Gte)
	}
}

func TestFilterFromMap_ShouldAndMustNot(t *testing.T) {
	in := map[string]any{
		"should": []map[string]any{
			{"key": "tag", "match": map[string]any{"value": "x"}},
		},
		"must_not": []map[string]any{
			{"key": "draft", "match": map[string]any{"value": true}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if len(f.Should) != 1 {
		t.Errorf("Should has %d, want 1", len(f.Should))
	}
	if len(f.MustNot) != 1 {
		t.Errorf("MustNot has %d, want 1", len(f.MustNot))
	}
	if got := f.MustNot[0].GetField().Match.GetBoolean(); got != true {
		t.Errorf("must_not bool match = %v, want true", got)
	}
}

func TestFilterFromMap_MatchAnyKeywords(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "category", "match": map[string]any{"any": []any{"a", "b", "c"}}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	c := f.Must[0].GetField()
	got := c.Match.GetKeywords()
	if got == nil {
		t.Fatal("expected keywords match")
	}
	if len(got.Strings) != 3 || got.Strings[0] != "a" {
		t.Errorf("keywords = %v, want [a b c]", got.Strings)
	}
}

func TestFilterFromMap_MatchAnyInts(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "id", "match": map[string]any{"any": []any{int64(1), int64(2)}}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	c := f.Must[0].GetField()
	got := c.Match.GetIntegers()
	if got == nil {
		t.Fatal("expected integers match")
	}
	if len(got.Integers) != 2 {
		t.Errorf("integers = %v, want length 2", got.Integers)
	}
}

func TestFilterFromMap_AnyShape(t *testing.T) {
	// Conditions arriving as []any (e.g., from JSON unmarshal) must work too.
	in := map[string]any{
		"must": []any{
			map[string]any{"key": "lang", "match": map[string]any{"value": "go"}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if len(f.Must) != 1 {
		t.Errorf("Must has %d, want 1", len(f.Must))
	}
}

func TestFilterFromMap_BadCondition(t *testing.T) {
	// Missing both 'match' and 'range' → error.
	in := map[string]any{
		"must": []map[string]any{
			{"key": "x"},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error for malformed condition")
	}
}

func TestFilterFromMap_BadMatchType(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "x", "match": map[string]any{"value": 3.14}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error for non-integer float match value")
	}
}

func TestFilterFromMap_RangeNeedsAtLeastOneOp(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "x", "range": map[string]any{}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error for empty range")
	}
}

func TestFilterFromMap_FloatMatchValueIntegralOK(t *testing.T) {
	// JSON ints come through as float64; we coerce when integral.
	in := map[string]any{
		"must": []map[string]any{
			{"key": "x", "match": map[string]any{"value": float64(7)}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if got := f.Must[0].GetField().Match.GetInteger(); got != 7 {
		t.Errorf("integer match = %v, want 7", got)
	}
}

// ---------------------------------------------------------------------------
// DocumentText — nil-Part skip path. The first Part is nil; the second is a
// real text part that must still be appended.
// ---------------------------------------------------------------------------

func TestDocumentText_NilPartSkipped(t *testing.T) {
	doc := &ai.Document{
		Content: []*ai.Part{
			nil,
			ai.NewTextPart("only"),
		},
	}
	if got := DocumentText(doc); got != "only" {
		t.Errorf("DocumentText = %q, want only", got)
	}
}

// ---------------------------------------------------------------------------
// DocumentID — nil document error path.
// ---------------------------------------------------------------------------

func TestDocumentID_NilDoc(t *testing.T) {
	if _, err := DocumentID(nil); err == nil {
		t.Errorf("expected error for nil document")
	}
}

func TestDocumentID_MarshalError(t *testing.T) {
	// A function value in Metadata makes json.Marshal fail; that should
	// surface as the wrapped "marshal document" error.
	doc := &ai.Document{
		Content:  []*ai.Part{ai.NewTextPart("x")},
		Metadata: map[string]any{"f": func() {}},
	}
	_, err := DocumentID(doc)
	if err == nil {
		t.Fatalf("expected marshal error")
	}
	if got := err.Error(); !strings.Contains(got, "marshal document") {
		t.Errorf("err = %q, want it to mention 'marshal document'", got)
	}
}

// ---------------------------------------------------------------------------
// ValueToAny — default branch for an unrecognized one-of kind. We construct a
// pristine *qclient.Value (no Kind set) so GetKind returns nil and we fall
// through to the default case.
// ---------------------------------------------------------------------------

func TestValueToAny_DefaultBranch(t *testing.T) {
	v := &qclient.Value{} // no Kind set → GetKind() returns nil → default case
	if got := ValueToAny(v); got != nil {
		t.Errorf("ValueToAny(empty Value) = %v (%T), want nil", got, got)
	}
}

// ---------------------------------------------------------------------------
// ScoredPointToDocument — metadata key holds a non-map value (e.g., a
// string). The function should fall through to the "flat payload" branch.
// ---------------------------------------------------------------------------

func TestScoredPointToDocument_MetadataNotMap(t *testing.T) {
	payload := qclient.NewValueMap(map[string]any{
		"content":  "doc text",
		"metadata": "not a map", // string under the metadata key
		"src":      "file.md",
	})
	sp := &qclient.ScoredPoint{Payload: payload, Score: 0.1}

	doc := ScoredPointToDocument(sp, "content", "metadata")
	if doc == nil {
		t.Fatal("ScoredPointToDocument returned nil")
	}
	if got := doc.Content[0].Text; got != "doc text" {
		t.Errorf("text = %q, want doc text", got)
	}
	// "metadata" was a string → fell into the flat-payload branch.
	if got := doc.Metadata["src"]; got != "file.md" {
		t.Errorf("metadata.src = %v, want file.md", got)
	}
}

// ---------------------------------------------------------------------------
// DocumentToPoint — error and fallback branches.
// ---------------------------------------------------------------------------

func TestDocumentToPoint_NilDoc(t *testing.T) {
	if _, err := DocumentToPoint(nil, []float32{0.1}, "", "content", "metadata"); err == nil {
		t.Errorf("expected error for nil document")
	}
}

func TestDocumentToPoint_DocumentIDError(t *testing.T) {
	// Metadata containing a func makes DocumentID's json.Marshal fail; that
	// error should propagate up through DocumentToPoint.
	doc := &ai.Document{
		Content:  []*ai.Part{ai.NewTextPart("x")},
		Metadata: map[string]any{"f": func() {}},
	}
	_, err := DocumentToPoint(doc, []float32{0.1}, "", "content", "metadata")
	if err == nil {
		t.Errorf("expected DocumentID error to propagate")
	}
}

func TestDocumentToPoint_NoMetadata(t *testing.T) {
	// A document with nil Metadata exercises the "doc.Metadata != nil" guard.
	doc := ai.DocumentFromText("only", nil)
	p, err := DocumentToPoint(doc, []float32{0.1}, "", "content", "metadata")
	if err != nil {
		t.Fatalf("DocumentToPoint: %v", err)
	}
	if _, ok := p.Payload["metadata"]; ok {
		t.Errorf("payload.metadata should be absent when doc has no metadata")
	}
}

func TestDocumentToPoint_JSONFallbackCoercion(t *testing.T) {
	// []int is rejected by TryValueMap (only []interface{} is supported),
	// but JSON-encodes cleanly. The DocumentToPoint fallback should
	// JSON-round-trip the metadata into a generic map[string]any and put it
	// onto the point.
	doc := ai.DocumentFromText("hello", map[string]any{
		"ids": []int{1, 2, 3},
	})
	p, err := DocumentToPoint(doc, []float32{0.1, 0.2}, "", "content", "metadata")
	if err != nil {
		t.Fatalf("DocumentToPoint: %v", err)
	}
	if got := p.Payload["content"].GetStringValue(); got != "hello" {
		t.Errorf("content = %q, want hello", got)
	}
	// The metadata key landed via the JSON fallback.
	if _, ok := p.Payload["metadata"]; !ok {
		t.Errorf("expected metadata to be present after JSON fallback coercion")
	}
}

// ---------------------------------------------------------------------------
// FilterFromMap — error branches in conditionsFromAny / conditionFromMap.
// ---------------------------------------------------------------------------

func TestFilterFromMap_AnyShape_ConditionNotMap(t *testing.T) {
	in := map[string]any{
		"must": []any{"not a map"},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when []any element is not a map")
	}
}

func TestFilterFromMap_AnyShape_NestedConditionError(t *testing.T) {
	in := map[string]any{
		"must": []any{
			map[string]any{"key": "x"}, // missing match/range
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected nested condition error")
	}
}

func TestFilterFromMap_TypedShape_NestedConditionError(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "x"}, // neither match nor range
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected nested condition error in typed-shape branch")
	}
}

func TestFilterFromMap_ConditionsWrongType(t *testing.T) {
	// A scalar where a slice of conditions was expected.
	in := map[string]any{"must": "scalar"}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error for non-slice conditions")
	}
}

func TestFilterFromMap_MatchNotMap(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "x", "match": "scalar"},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when match is not a map")
	}
}

func TestFilterFromMap_MatchMissingKey(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"match": map[string]any{"value": "x"}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when 'key' is missing on a match condition")
	}
}

func TestFilterFromMap_RangeNotMap(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "x", "range": "scalar"},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when range is not a map")
	}
}

func TestFilterFromMap_RangeMissingKey(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"range": map[string]any{"gt": 1}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when 'key' is missing on a range condition")
	}
}

// ---------------------------------------------------------------------------
// matchCondition value coercion — exercise every supported scalar type plus
// the no-key/missing-shape error branches.
// ---------------------------------------------------------------------------

func TestFilterFromMap_MatchBool(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"value": false}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if got := f.Must[0].GetField().Match.GetBoolean(); got {
		t.Errorf("match bool = %v, want false", got)
	}
}

func TestFilterFromMap_MatchInt(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"value": int(42)}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if got := f.Must[0].GetField().Match.GetInteger(); got != 42 {
		t.Errorf("match int = %v, want 42", got)
	}
}

func TestFilterFromMap_MatchInt32(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"value": int32(11)}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if got := f.Must[0].GetField().Match.GetInteger(); got != 11 {
		t.Errorf("match int32 = %v, want 11", got)
	}
}

func TestFilterFromMap_MatchInt64(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"value": int64(99)}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	if got := f.Must[0].GetField().Match.GetInteger(); got != 99 {
		t.Errorf("match int64 = %v, want 99", got)
	}
}

func TestFilterFromMap_MatchUnsupportedType(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"value": []byte("nope")}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error for unsupported match value type")
	}
}

func TestFilterFromMap_MatchMissingValueAndAny(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when match has neither 'value' nor 'any'")
	}
}

// ---------------------------------------------------------------------------
// matchCondition's "any" branch — every numeric variant + error paths.
// ---------------------------------------------------------------------------

func TestFilterFromMap_MatchAnyNotSlice(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"any": "scalar"}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when match.any is not a slice")
	}
}

func TestFilterFromMap_MatchAnyEmpty(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"any": []any{}}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when match.any is empty")
	}
}

func TestFilterFromMap_MatchAnyKeywordsMixed(t *testing.T) {
	// A non-string slipped into a keyword list → error.
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"any": []any{"a", 1}}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when keyword slice mixes types")
	}
}

func TestFilterFromMap_MatchAnyIntsMixed(t *testing.T) {
	// Stage int as the inferred element type via the first entry; then a
	// non-numeric in the tail → error.
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"any": []any{int(1), "two"}}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error when int slice contains non-numeric")
	}
}

func TestFilterFromMap_MatchAnyAllIntKinds(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"any": []any{int(1), int32(2), int64(3), float64(4)}}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	got := f.Must[0].GetField().Match.GetIntegers()
	if got == nil || len(got.Integers) != 4 {
		t.Fatalf("integers = %v, want length 4", got)
	}
	want := []int64{1, 2, 3, 4}
	for i, w := range want {
		if got.Integers[i] != w {
			t.Errorf("integers[%d] = %d, want %d", i, got.Integers[i], w)
		}
	}
}

func TestFilterFromMap_MatchAnyUnsupportedElement(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "match": map[string]any{"any": []any{true}}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error for unsupported any-element type")
	}
}

// ---------------------------------------------------------------------------
// rangeCondition — every operator branch + error paths.
// ---------------------------------------------------------------------------

func TestFilterFromMap_RangeAllOps(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "range": map[string]any{"gt": 1, "gte": 2, "lt": 9, "lte": 10}},
		},
	}
	f, err := FilterFromMap(in)
	if err != nil {
		t.Fatalf("FilterFromMap: %v", err)
	}
	r := f.Must[0].GetField().Range
	if r.Gt == nil || *r.Gt != 1 {
		t.Errorf("Gt = %v, want 1", r.Gt)
	}
	if r.Gte == nil || *r.Gte != 2 {
		t.Errorf("Gte = %v, want 2", r.Gte)
	}
	if r.Lt == nil || *r.Lt != 9 {
		t.Errorf("Lt = %v, want 9", r.Lt)
	}
	if r.Lte == nil || *r.Lte != 10 {
		t.Errorf("Lte = %v, want 10", r.Lte)
	}
}

func TestFilterFromMap_RangeBadValueType(t *testing.T) {
	in := map[string]any{
		"must": []map[string]any{
			{"key": "k", "range": map[string]any{"gt": "nope"}},
		},
	}
	if _, err := FilterFromMap(in); err == nil {
		t.Errorf("expected error for non-numeric range value")
	}
}

// ---------------------------------------------------------------------------
// toFloat64 — all numeric kinds.
// ---------------------------------------------------------------------------

func TestFilterFromMap_RangeNumericKinds(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want float64
	}{
		{"int", int(1), 1},
		{"int32", int32(2), 2},
		{"int64", int64(3), 3},
		{"float32", float32(4.5), 4.5},
		{"float64", float64(5.5), 5.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := map[string]any{
				"must": []map[string]any{
					{"key": "k", "range": map[string]any{"gt": tc.v}},
				},
			}
			f, err := FilterFromMap(in)
			if err != nil {
				t.Fatalf("FilterFromMap: %v", err)
			}
			r := f.Must[0].GetField().Range
			if r.Gt == nil || *r.Gt != tc.want {
				t.Errorf("Gt = %v, want %v", r.Gt, tc.want)
			}
		})
	}
}
