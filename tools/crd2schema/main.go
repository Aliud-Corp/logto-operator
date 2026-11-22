// Command crd2schema extracts the JSON Schema of a logtoctl document from the generated CRD, so the CLI file and the
// CR share one schema written once (api/v1alpha1). The OpenAPI v3 schema of the CRD's first version is kept as is;
// apiVersion and kind are pinned to the CRD's group, version and kind, so an editor can validate a document offline.
//
// Usage: go run ./tools/crd2schema <crd.yaml> <out.schema.json>
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"sigs.k8s.io/yaml"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: crd2schema <crd.yaml> <out.schema.json>")
		os.Exit(2)
	}
	in, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "crd2schema:", err)
		os.Exit(1)
	}
	out, err := Extract(in)
	if err != nil {
		fmt.Fprintln(os.Stderr, "crd2schema:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[2], out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "crd2schema:", err)
		os.Exit(1)
	}
}

type crd struct {
	Spec struct {
		Group string `json:"group"`
		Names struct {
			Kind string `json:"kind"`
		} `json:"names"`
		Versions []struct {
			Name   string `json:"name"`
			Schema struct {
				OpenAPIV3Schema map[string]any `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

// Extract returns the JSON Schema (indented, keys sorted) of the CRD's first version.
func Extract(crdYAML []byte) ([]byte, error) {
	var c crd
	if err := yaml.Unmarshal(crdYAML, &c); err != nil {
		return nil, fmt.Errorf("decode CRD: %w", err)
	}
	if len(c.Spec.Versions) == 0 || c.Spec.Versions[0].Schema.OpenAPIV3Schema == nil {
		return nil, errors.New("the CRD has no version with an openAPIV3Schema")
	}
	v := c.Spec.Versions[0]
	schema := v.Schema.OpenAPIV3Schema
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil, errors.New("the schema has no properties")
	}
	pin := func(name, value string) {
		p, _ := props[name].(map[string]any)
		if p == nil {
			p = map[string]any{"type": "string"}
		}
		p["enum"] = []any{value}
		props[name] = p
	}
	pin("apiVersion", c.Spec.Group+"/"+v.Name)
	pin("kind", c.Spec.Names.Kind)
	delete(props, "status") // written by the operator, never by an author
	schema["$schema"] = "http://json-schema.org/draft-07/schema#"
	schema["title"] = c.Spec.Names.Kind + " (" + c.Spec.Group + "/" + v.Name + ")"
	req, _ := schema["required"].([]any)
	schema["required"] = appendMissing(req, "apiVersion", "kind", "spec")

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(schema); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func appendMissing(list []any, values ...string) []any {
	have := map[string]bool{}
	for _, v := range list {
		if s, ok := v.(string); ok {
			have[s] = true
		}
	}
	for _, v := range values {
		if !have[v] {
			list = append(list, v)
		}
	}
	return list
}
