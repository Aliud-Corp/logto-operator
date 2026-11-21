// Package openapi reads a Logto OpenAPI document (vendored or served by an instance) and answers the two questions
// the engine asks of it: is this body valid for that operation, and does that operation know this field. The
// contract tests ask them of every vendored spec of the support window; the capability check asks them of the
// target instance's own spec.
package openapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/Aliud-Corp/logto-operator/internal/logto/specnorm"
)

// Doc is one loaded spec, indexed by operationId.
type Doc struct {
	ops map[string]*openapi3.Operation
}

// Load normalizes (see specnorm) and loads a spec. It does not validate the document itself: Logto's documents use
// formats and constructs a strict validation refuses, and only the schemas of the operations matter here.
func Load(raw []byte) (*Doc, error) {
	norm, _, err := specnorm.Normalize(raw, nil)
	if err != nil {
		return nil, err
	}
	if norm, err = goPatterns(norm); err != nil {
		return nil, err
	}
	loader := openapi3.NewLoader()
	loader.Context = context.Background()
	t, err := loader.LoadFromData(norm)
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	d := &Doc{ops: map[string]*openapi3.Operation{}}
	for _, item := range t.Paths.Map() {
		for _, op := range item.Operations() {
			if op.OperationID != "" {
				d.ops[op.OperationID] = op
			}
		}
	}
	return d, nil
}

// Has reports whether the operation exists.
func (d *Doc) Has(operationID string) bool {
	_, ok := d.ops[operationID]
	return ok
}

// RequestSchema is the JSON request body schema of an operation, or nil.
func (d *Doc) RequestSchema(operationID string) (*openapi3.Schema, error) {
	op, ok := d.ops[operationID]
	if !ok {
		return nil, fmt.Errorf("operation %s does not exist in this spec", operationID)
	}
	if op.RequestBody == nil || op.RequestBody.Value == nil {
		return nil, nil
	}
	mt := op.RequestBody.Value.Content.Get("application/json")
	if mt == nil || mt.Schema == nil {
		return nil, nil
	}
	return mt.Schema.Value, nil
}

// ResponseSchema is the JSON schema of one response of an operation, or nil.
func (d *Doc) ResponseSchema(operationID string, status int) (*openapi3.Schema, error) {
	op, ok := d.ops[operationID]
	if !ok {
		return nil, fmt.Errorf("operation %s does not exist in this spec", operationID)
	}
	r := op.Responses.Status(status)
	if r == nil || r.Value == nil {
		return nil, fmt.Errorf("operation %s has no %d response", operationID, status)
	}
	mt := r.Value.Content.Get("application/json")
	if mt == nil || mt.Schema == nil {
		return nil, nil
	}
	return mt.Schema.Value, nil
}

// Validate checks a decoded JSON value against a schema: types, required properties and enums (kin-openapi), and,
// stricter than the schema itself, every object key must be a property the schema declares. Logto's schemas rarely
// close their objects, and a key a version does not know is exactly what a contract test must catch.
func Validate(schema *openapi3.Schema, value any) []string {
	if schema == nil {
		return []string{"no schema"}
	}
	var problems []string
	if err := schema.VisitJSON(value, openapi3.MultiErrors()); err != nil {
		problems = append(problems, schemaErrors(err)...)
	}
	problems = append(problems, unknownKeys(schema, value, "")...)
	sort.Strings(problems)
	return dedupe(problems)
}

// schemaErrors flattens kin-openapi's errors into "path: reason" lines.
func schemaErrors(err error) []string {
	var me openapi3.MultiError
	if errors.As(err, &me) {
		var out []string
		for _, e := range me {
			out = append(out, schemaErrors(e)...)
		}
		return out
	}
	var se *openapi3.SchemaError
	if errors.As(err, &se) {
		path := strings.Join(se.JSONPointer(), ".")
		if path == "" {
			path = "(body)"
		}
		return []string{path + ": " + se.Reason}
	}
	return []string{err.Error()}
}

// jsRegex is a JavaScript regular expression literal, as Logto's zod schemas emit them: /source/flags.
var jsRegex = regexp.MustCompile(`^/(.*)/([dgimsuvy]*)$`)

// goPatterns rewrites every `pattern` written as a JavaScript literal into a Go regular expression (flags i, m, s
// become inline groups; g, u, y, d, v do not change what matches), and reads every `oneOf` as `anyOf`: Logto emits
// zod unions as oneOf, but a union accepts a value matching several branches (a URL is both a `url` and a string the
// `empty` format does not constrain), which a strict oneOf refuses.
func goPatterns(raw []byte) ([]byte, error) {
	var doc any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if p, ok := v["pattern"].(string); ok {
				if m := jsRegex.FindStringSubmatch(p); m != nil {
					flags := ""
					for _, f := range "ims" {
						if strings.ContainsRune(m[2], f) {
							flags += string(f)
						}
					}
					if flags != "" {
						v["pattern"] = "(?" + flags + ")" + m[1]
					} else {
						v["pattern"] = m[1]
					}
				}
			}
			if one, ok := v["oneOf"]; ok {
				v["anyOf"] = one
				delete(v, "oneOf")
			}
			for _, c := range v {
				walk(c)
			}
		case []any:
			for _, c := range v {
				walk(c)
			}
		}
	}
	walk(doc)
	return json.Marshal(doc)
}

func unknownKeys(s *openapi3.Schema, value any, path string) []string {
	if s == nil {
		return nil
	}
	// A union accepts the value when one of its branches does: report the problems of the branch that fits best.
	if branches := append(append(openapi3.SchemaRefs{}, s.AnyOf...), s.OneOf...); len(branches) > 0 && len(s.Properties) == 0 {
		var best []string
		for i, ref := range branches {
			problems := unknownKeys(ref.Value, value, path)
			if ref.Value != nil {
				if err := ref.Value.VisitJSON(value, openapi3.MultiErrors()); err != nil {
					problems = append(problems, schemaErrors(err)...)
				}
			}
			if len(problems) == 0 {
				return nil
			}
			if i == 0 || len(problems) < len(best) {
				best = problems
			}
		}
		return best
	}
	switch v := value.(type) {
	case map[string]any:
		var out []string
		for k, child := range v {
			p := join(path, k)
			cs := property(s, k)
			if cs == nil {
				if !openObject(s) {
					out = append(out, p+": property unknown to this version")
				}
				continue
			}
			out = append(out, unknownKeys(cs, child, p)...)
		}
		return out
	case []any:
		items := arrayItems(s)
		var out []string
		for i, child := range v {
			out = append(out, unknownKeys(items, child, path+"["+strconv.Itoa(i)+"]")...)
		}
		return out
	}
	return nil
}

// ValidateSchema is Validate for a schema obtained as any (nil schema: the operation has no JSON body).
func ValidateSchema(schema any, value any) []string {
	s, _ := schema.(*openapi3.Schema)
	return Validate(s, value)
}

// HasPath reports whether a property path (a.b.c; arrays are traversed implicitly) exists in a schema.
func HasPath(s *openapi3.Schema, path string) bool {
	cur := s
	for _, part := range strings.Split(path, ".") {
		for cur != nil && (cur.Type.Is("array") || cur.Items != nil) && property(cur, part) == nil {
			cur = arrayItems(cur)
		}
		if cur == nil {
			return false
		}
		cur = property(cur, part)
		if cur == nil {
			return false
		}
	}
	return true
}

// property finds a property, looking through allOf / oneOf / anyOf branches.
func property(s *openapi3.Schema, name string) *openapi3.Schema {
	if s == nil {
		return nil
	}
	if p, ok := s.Properties[name]; ok && p.Value != nil {
		return p.Value
	}
	for _, group := range []openapi3.SchemaRefs{s.AllOf, s.OneOf, s.AnyOf} {
		for _, ref := range group {
			if p := property(ref.Value, name); p != nil {
				return p
			}
		}
	}
	return nil
}

func arrayItems(s *openapi3.Schema) *openapi3.Schema {
	if s == nil {
		return nil
	}
	if s.Items != nil {
		return s.Items.Value
	}
	for _, group := range []openapi3.SchemaRefs{s.OneOf, s.AnyOf} {
		for _, ref := range group {
			if ref.Value != nil && ref.Value.Items != nil {
				return ref.Value.Items.Value
			}
		}
	}
	return nil
}

// openObject is an object with no declared property at all, or one declaring additional properties: any key goes.
func openObject(s *openapi3.Schema) bool {
	if s.AdditionalProperties.Has != nil && *s.AdditionalProperties.Has {
		return true
	}
	if s.AdditionalProperties.Schema != nil {
		return true
	}
	return len(s.Properties) == 0 && len(s.AllOf) == 0 && len(s.OneOf) == 0 && len(s.AnyOf) == 0
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func dedupe(in []string) []string {
	out := in[:0]
	for i, s := range in {
		if i == 0 || s != in[i-1] {
			out = append(out, s)
		}
	}
	return out
}
