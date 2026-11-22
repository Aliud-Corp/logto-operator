// Package specnorm turns Logto's vendored Management API OpenAPI document into the input of oapi-codegen.
//
// The vendored files (api/logto/<version>.openapi.json) stay byte-identical to what each Logto version serves:
// they are sources, and a source is never edited by hand. Logto's generator, however, encodes a zod literal as a JSON
// string inside `format` (`{"type":"string","format":"\"Social\""}`), which is not OpenAPI and which kin-openapi (the
// loader of oapi-codegen) rejects outright: "format ... may not contain a double quote". This program rewrites
// exactly that construct into what it means, a one-value `enum`. It also leaves unconstrained the fields Logto
// exports as an empty object "Validator function" (custom zod checks on what is really a string).
//
// Second, a short list of OVERLAYS (see `Overlays`): extensions added at an exact JSON pointer to work around a
// generator limitation, each with its reason. An overlay whose target no longer exists, or no longer looks like what
// it was written for, FAILS the run: after a Logto upgrade it is removed instead of silently patching something else.
//
// Output is deterministic (object keys sorted, numbers kept verbatim), so the generated client depends only on the
// vendored spec and the pinned generator.
//
// The command tools/specnorm runs it before oapi-codegen; the contract tests and the capability check run it before
// loading a spec with kin-openapi.
package specnorm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Normalize rewrites every schema whose `format` is a JSON-encoded literal into a one-value `enum`, and returns the
// re-encoded document, with the overlays applied, and the number of literal formats it rewrote.
func Normalize(raw []byte, list []Overlay) ([]byte, int, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, 0, fmt.Errorf("decode: %w", err)
	}
	n, err := walk(doc)
	if err != nil {
		return nil, 0, err
	}
	if err := applyOverlays(doc, list); err != nil {
		return nil, 0, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", " ")
	if err := enc.Encode(doc); err != nil {
		return nil, 0, fmt.Errorf("encode: %w", err)
	}
	return buf.Bytes(), n, nil
}

func walk(node any) (int, error) {
	count := 0
	switch v := node.(type) {
	case map[string]any:
		// A field checked by a custom zod function is exported as an empty object described "Validator function",
		// though its value is a string (a redirect URI, a name...): real answers would not decode, valid bodies
		// would be refused. It is left unconstrained.
		if v["description"] == "Validator function" && v["type"] == "object" && v["properties"] == nil {
			delete(v, "type")
			delete(v, "additionalProperties")
		}
		if f, ok := v["format"].(string); ok && strings.HasPrefix(f, `"`) {
			var literal string
			if err := json.Unmarshal([]byte(f), &literal); err != nil {
				return 0, fmt.Errorf("format %q is quoted but not a JSON string: %w", f, err)
			}
			v["enum"] = []any{literal}
			delete(v, "format")
			count++
		}
		for _, child := range v {
			n, err := walk(child)
			if err != nil {
				return 0, err
			}
			count += n
		}
	case []any:
		for _, child := range v {
			n, err := walk(child)
			if err != nil {
				return 0, err
			}
			count += n
		}
	}
	return count, nil
}

// Overlay sets Key to Value on the object at Pointer (RFC 6901), which must hold every key of Expect.
type Overlay struct {
	Pointer string
	Expect  []string
	Key     string
	Value   any
	Reason  string
}

// Overlays are the overlays the generator needs, each with its reason.
var Overlays = []Overlay{
	{
		Pointer: "/paths/~1api~1applications/get/parameters/0/schema/oneOf/0/items",
		Expect:  []string{"enum"},
		Key:     "x-go-type-name",
		Value:   "ListApplicationsParamsTypesItem",
		Reason: "oapi-codegen 2.8 names both the array alias and its inline enum item ListApplicationsParamsTypes0 " +
			"(oneOf [array of enum, enum]), which does not compile",
	},
}

func applyOverlays(doc any, list []Overlay) error {
	for _, o := range list {
		node, err := resolve(doc, o.Pointer)
		if err != nil {
			return fmt.Errorf("overlay %s: %w (remove it if Logto changed this schema)", o.Pointer, err)
		}
		obj, ok := node.(map[string]any)
		if !ok {
			return fmt.Errorf("overlay %s: target is not an object", o.Pointer)
		}
		for _, k := range o.Expect {
			if _, has := obj[k]; !has {
				return fmt.Errorf("overlay %s: target has no %q any more (remove the overlay if Logto changed it)", o.Pointer, k)
			}
		}
		obj[o.Key] = o.Value
	}
	return nil
}

func resolve(doc any, pointer string) (any, error) {
	node := doc
	for _, raw := range strings.Split(strings.TrimPrefix(pointer, "/"), "/") {
		token := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
		switch v := node.(type) {
		case map[string]any:
			child, ok := v[token]
			if !ok {
				return nil, fmt.Errorf("no key %q", token)
			}
			node = child
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(v) {
				return nil, fmt.Errorf("no index %q", token)
			}
			node = v[i]
		default:
			return nil, fmt.Errorf("cannot descend into %q", token)
		}
	}
	return node, nil
}
