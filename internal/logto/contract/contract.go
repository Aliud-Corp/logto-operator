// Package contract checks a kind's own wire types against every vendored Logto spec of the support window: each body
// a kind can send, and each response it decodes, must be valid for every supported version. It replaces the type
// safety generated request and response types would give, version by version.
package contract

import (
	"encoding/json"
	"fmt"
	"sync"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/internal/logto/openapi"
)

var (
	once   sync.Once
	docs   map[string]*openapi.Doc
	errAll error
)

func window() (map[string]*openapi.Doc, error) {
	once.Do(func() {
		docs = map[string]*openapi.Doc{}
		for _, v := range logtospecs.Versions() {
			raw, err := logtospecs.Spec(v)
			if err != nil {
				errAll = err
				return
			}
			d, err := openapi.Load(raw)
			if err != nil {
				errAll = fmt.Errorf("vendored spec %s: %w", v, err)
				return
			}
			docs[v] = d
		}
	})
	return docs, errAll
}

// Problems maps a version to what is wrong for it; an empty map means valid for the whole window.
type Problems map[string][]string

// Request validates a body (a wire value, marshalled as the kind sends it) for an operation, per version.
func Request(operationID string, body any) (Problems, error) {
	return check(body, func(d *openapi.Doc) (any, error) { return d.RequestSchema(operationID) })
}

// Response validates a response fixture for an operation and status, per version.
func Response(operationID string, status int, body any) (Problems, error) {
	return check(body, func(d *openapi.Doc) (any, error) { return d.ResponseSchema(operationID, status) })
}

func check(body any, schemaOf func(*openapi.Doc) (any, error)) (Problems, error) {
	all, err := window()
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	out := Problems{}
	for v, d := range all {
		s, err := schemaOf(d)
		if err != nil {
			out[v] = []string{err.Error()}
			continue
		}
		if p := openapi.ValidateSchema(s, value); len(p) > 0 {
			out[v] = p
		}
	}
	return out, nil
}
