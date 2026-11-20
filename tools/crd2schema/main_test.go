package main

import (
	"encoding/json"
	"testing"
)

const crdFixture = `
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
spec:
  group: example.test
  names:
    kind: Thing
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        type: object
        required: [spec]
        properties:
          apiVersion: {type: string}
          kind: {type: string}
          spec: {type: object, properties: {size: {type: integer}}}
          status: {type: object}
`

func TestExtractPinsTheKindAndDropsTheStatus(t *testing.T) {
	out, err := Extract([]byte(crdFixture))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Schema     string                    `json:"$schema"`
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(out, &s); err != nil {
		t.Fatal(err)
	}
	if s.Schema == "" {
		t.Error("no $schema")
	}
	if got := s.Properties["apiVersion"]["enum"]; len(got.([]any)) != 1 || got.([]any)[0] != "example.test/v1" {
		t.Errorf("apiVersion enum = %v", got)
	}
	if got := s.Properties["kind"]["enum"]; got.([]any)[0] != "Thing" {
		t.Errorf("kind enum = %v", got)
	}
	if _, has := s.Properties["status"]; has {
		t.Error("status kept: authors never write it")
	}
	if len(s.Required) != 3 {
		t.Errorf("required = %v, want spec, apiVersion, kind", s.Required)
	}
}

func TestExtractRefusesACRDWithoutSchema(t *testing.T) {
	if _, err := Extract([]byte("spec: {versions: []}")); err == nil {
		t.Fatal("want an error")
	}
}
