// Package convert handles translation between Genkit's data model and Qdrant's
// gRPC types: filters, payload values, points, and documents.
package convert

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/firebase/genkit/go/ai"
	qclient "github.com/qdrant/go-client/qdrant"
)

// DocumentText concatenates all text Parts of a Document into a single string.
// This mirrors what other Genkit Go vector plugins do when storing document
// content as a single payload field.
func DocumentText(doc *ai.Document) string {
	if doc == nil {
		return ""
	}
	var sb strings.Builder
	for _, p := range doc.Content {
		if p == nil {
			continue
		}
		if p.IsText() {
			sb.WriteString(p.Text)
		}
	}
	return sb.String()
}

// DocumentID returns a stable ID for a Document derived from its JSON
// representation. We hash the document so that re-indexing the same doc
// overwrites the previous point rather than producing duplicates.
//
// The returned ID is a deterministic UUID string suitable for Qdrant point IDs
// (Qdrant accepts either uint64 or UUID string IDs).
func DocumentID(doc *ai.Document) (string, error) {
	if doc == nil {
		return "", fmt.Errorf("convert: nil document")
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return "", fmt.Errorf("convert: marshal document: %w", err)
	}
	sum := md5.Sum(b)
	// Format as RFC 4122 UUID. We don't bother setting the version bits
	// because Qdrant only cares that the ID is a valid UUID string.
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16]), nil
}

// ValueToAny converts a Qdrant payload Value back into a plain Go any.
// Returns nil for nil/null values.
func ValueToAny(v *qclient.Value) any {
	if v == nil {
		return nil
	}
	switch k := v.GetKind().(type) {
	case *qclient.Value_NullValue:
		return nil
	case *qclient.Value_BoolValue:
		return k.BoolValue
	case *qclient.Value_IntegerValue:
		return k.IntegerValue
	case *qclient.Value_DoubleValue:
		return k.DoubleValue
	case *qclient.Value_StringValue:
		return k.StringValue
	case *qclient.Value_StructValue:
		out := make(map[string]any, len(k.StructValue.GetFields()))
		for kk, vv := range k.StructValue.GetFields() {
			out[kk] = ValueToAny(vv)
		}
		return out
	case *qclient.Value_ListValue:
		vals := k.ListValue.GetValues()
		out := make([]any, 0, len(vals))
		for _, vv := range vals {
			out = append(out, ValueToAny(vv))
		}
		return out
	default:
		return nil
	}
}

// PayloadToMap converts a Qdrant payload (map[string]*Value) to map[string]any.
func PayloadToMap(payload map[string]*qclient.Value) map[string]any {
	if payload == nil {
		return nil
	}
	out := make(map[string]any, len(payload))
	for k, v := range payload {
		out[k] = ValueToAny(v)
	}
	return out
}

// ScoredPointToDocument turns a Qdrant search hit into an ai.Document.
//
// contentKey is the payload key that holds the document text; metadataKey is
// the payload key that holds the metadata map. Both keys are removed from the
// returned document's Metadata to avoid duplicating storage.
//
// The point's similarity score is preserved in Metadata under the key
// "_score" so callers can rank or filter results client-side.
func ScoredPointToDocument(p *qclient.ScoredPoint, contentKey, metadataKey string) *ai.Document {
	if p == nil {
		return nil
	}
	payload := PayloadToMap(p.GetPayload())

	var text string
	if v, ok := payload[contentKey].(string); ok {
		text = v
	}

	var metadata map[string]any
	if m, ok := payload[metadataKey].(map[string]any); ok {
		metadata = m
	} else {
		// No nested metadata map; surface the rest of the payload (minus
		// the content key) as metadata. This is friendlier when documents
		// were written by another tool that didn't nest metadata.
		metadata = make(map[string]any)
		for k, v := range payload {
			if k == contentKey {
				continue
			}
			metadata[k] = v
		}
	}
	if metadata == nil {
		metadata = make(map[string]any)
	}
	metadata["_score"] = p.GetScore()

	return ai.DocumentFromText(text, metadata)
}

// DocumentToPoint builds a Qdrant PointStruct for the given document and
// embedding vector. If vectorName is non-empty, the vector is stored as a
// named vector inside the point (for collections with multiple vector slots).
func DocumentToPoint(doc *ai.Document, embedding []float32, vectorName, contentKey, metadataKey string) (*qclient.PointStruct, error) {
	if doc == nil {
		return nil, fmt.Errorf("convert: nil document")
	}
	id, err := DocumentID(doc)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		contentKey: DocumentText(doc),
	}
	if doc.Metadata != nil {
		// Best-effort copy: skip values that aren't representable in
		// Qdrant's payload schema (e.g. functions, channels). The Qdrant
		// client panics on unsupported types via NewValueMap, so we
		// pre-validate with TryValueMap.
		safe, err := qclient.TryValueMap(doc.Metadata)
		if err == nil && safe != nil {
			payload[metadataKey] = doc.Metadata
		} else {
			// Fall back to a JSON round-trip to coerce types.
			b, jerr := json.Marshal(doc.Metadata)
			if jerr == nil {
				var coerced map[string]any
				if json.Unmarshal(b, &coerced) == nil {
					payload[metadataKey] = coerced
				}
			}
		}
	}

	point := &qclient.PointStruct{
		Id:      qclient.NewID(id),
		Payload: qclient.NewValueMap(payload),
	}

	if vectorName != "" {
		point.Vectors = qclient.NewVectorsMap(map[string]*qclient.Vector{
			vectorName: qclient.NewVector(embedding...),
		})
	} else {
		point.Vectors = qclient.NewVectors(embedding...)
	}
	return point, nil
}

// FilterFromMap translates a filter described as a generic map[string]any (the
// same JSON shape Qdrant's REST API accepts) into a *qclient.Filter for use
// with gRPC.
//
// Supported top-level keys: "must", "should", "must_not". Each value is a
// slice of condition maps. Each condition supports:
//   - {"key": "<field>", "match": {"value": <string|int|bool>}}
//   - {"key": "<field>", "match": {"any": [<string|int>...]}}
//   - {"key": "<field>", "range": {"gt|gte|lt|lte": <number>}}
//
// Anything more exotic (geo, datetime range, nested) should be expressed by
// constructing a *qclient.Filter directly via the public Qdrant Go client and
// passing it as the Filter option.
//
// Returns nil if the input is nil or empty.
func FilterFromMap(m map[string]any) (*qclient.Filter, error) {
	if len(m) == 0 {
		return nil, nil
	}
	f := &qclient.Filter{}

	for _, group := range []struct {
		key  string
		into *[]*qclient.Condition
	}{
		{"must", &f.Must},
		{"should", &f.Should},
		{"must_not", &f.MustNot},
	} {
		raw, ok := m[group.key]
		if !ok {
			continue
		}
		conds, err := conditionsFromAny(raw)
		if err != nil {
			return nil, fmt.Errorf("convert: %s: %w", group.key, err)
		}
		*group.into = conds
	}
	return f, nil
}

func conditionsFromAny(raw any) ([]*qclient.Condition, error) {
	// Accept []map[string]any (the natural Go literal form) and []any (what
	// you get from json.Unmarshal-ing a generic JSON document).
	switch v := raw.(type) {
	case []map[string]any:
		out := make([]*qclient.Condition, 0, len(v))
		for i, c := range v {
			cond, err := conditionFromMap(c)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out = append(out, cond)
		}
		return out, nil
	case []any:
		out := make([]*qclient.Condition, 0, len(v))
		for i, c := range v {
			cm, ok := c.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("[%d]: condition must be a map, got %T", i, c)
			}
			cond, err := conditionFromMap(cm)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out = append(out, cond)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected slice of conditions, got %T", raw)
	}
}

func conditionFromMap(c map[string]any) (*qclient.Condition, error) {
	key, _ := c["key"].(string)

	if matchAny, ok := c["match"]; ok {
		match, ok := matchAny.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("match must be a map, got %T", matchAny)
		}
		if key == "" {
			return nil, fmt.Errorf("match condition requires 'key'")
		}
		return matchCondition(key, match)
	}

	if rangeAny, ok := c["range"]; ok {
		rangeMap, ok := rangeAny.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("range must be a map, got %T", rangeAny)
		}
		if key == "" {
			return nil, fmt.Errorf("range condition requires 'key'")
		}
		return rangeCondition(key, rangeMap)
	}

	return nil, fmt.Errorf("condition has neither 'match' nor 'range'; advanced filters should be passed as a *qdrant.Filter directly")
}

func matchCondition(key string, m map[string]any) (*qclient.Condition, error) {
	if v, ok := m["value"]; ok {
		switch tv := v.(type) {
		case string:
			return qclient.NewMatchKeyword(key, tv), nil
		case bool:
			return qclient.NewMatchBool(key, tv), nil
		case int:
			return qclient.NewMatchInt(key, int64(tv)), nil
		case int32:
			return qclient.NewMatchInt(key, int64(tv)), nil
		case int64:
			return qclient.NewMatchInt(key, tv), nil
		case float64:
			// JSON numbers come through as float64; coerce to int64
			// when the value is integral.
			if tv == float64(int64(tv)) {
				return qclient.NewMatchInt(key, int64(tv)), nil
			}
			return nil, fmt.Errorf("match value %v: floats are not supported", tv)
		default:
			return nil, fmt.Errorf("match value: unsupported type %T", v)
		}
	}
	if anyAny, ok := m["any"]; ok {
		// match-any: list of keywords or ints
		anySlice, ok := anyAny.([]any)
		if !ok {
			return nil, fmt.Errorf("match.any must be a slice, got %T", anyAny)
		}
		if len(anySlice) == 0 {
			return nil, fmt.Errorf("match.any must be non-empty")
		}
		// All elements must be of the same scalar type. Inspect the first.
		switch anySlice[0].(type) {
		case string:
			ks := make([]string, 0, len(anySlice))
			for i, e := range anySlice {
				s, ok := e.(string)
				if !ok {
					return nil, fmt.Errorf("match.any[%d]: expected string, got %T", i, e)
				}
				ks = append(ks, s)
			}
			return qclient.NewMatchKeywords(key, ks...), nil
		case float64, int, int32, int64:
			ints := make([]int64, 0, len(anySlice))
			for i, e := range anySlice {
				switch tv := e.(type) {
				case int:
					ints = append(ints, int64(tv))
				case int32:
					ints = append(ints, int64(tv))
				case int64:
					ints = append(ints, tv)
				case float64:
					ints = append(ints, int64(tv))
				default:
					return nil, fmt.Errorf("match.any[%d]: expected int, got %T", i, e)
				}
			}
			return qclient.NewMatchInts(key, ints...), nil
		default:
			return nil, fmt.Errorf("match.any: unsupported element type %T", anySlice[0])
		}
	}
	return nil, fmt.Errorf("match condition needs 'value' or 'any'")
}

func rangeCondition(key string, m map[string]any) (*qclient.Condition, error) {
	r := &qclient.Range{}
	for _, op := range []string{"gt", "gte", "lt", "lte"} {
		v, ok := m[op]
		if !ok {
			continue
		}
		f, err := toFloat64(v)
		if err != nil {
			return nil, fmt.Errorf("range.%s: %w", op, err)
		}
		switch op {
		case "gt":
			r.Gt = &f
		case "gte":
			r.Gte = &f
		case "lt":
			r.Lt = &f
		case "lte":
			r.Lte = &f
		}
	}
	if r.Gt == nil && r.Gte == nil && r.Lt == nil && r.Lte == nil {
		return nil, fmt.Errorf("range condition needs at least one of gt/gte/lt/lte")
	}
	return qclient.NewRange(key, r), nil
}

func toFloat64(v any) (float64, error) {
	switch tv := v.(type) {
	case int:
		return float64(tv), nil
	case int32:
		return float64(tv), nil
	case int64:
		return float64(tv), nil
	case float32:
		return float64(tv), nil
	case float64:
		return tv, nil
	default:
		return 0, fmt.Errorf("expected number, got %T", v)
	}
}
