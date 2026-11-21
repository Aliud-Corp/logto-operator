// Package capability answers, before any write, whether the Logto being configured accepts what a kind is about to
// send: it reads the instance's own OpenAPI document (GET /api/swagger.json) and looks up the operation and the
// property paths. Logto exposes no version number through its API (its spec says `info.version: Cloud`); its spec is
// the authoritative list of what it accepts.
package capability

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
	"github.com/Aliud-Corp/logto-operator/internal/logto/openapi"
)

// Spec is the target instance's own spec.
type Spec struct {
	doc *openapi.Doc
	// Version is the supported Logto version whose vendored spec this one is, "" for a Logto outside the support
	// window (or a modified one): its behaviour was never tested.
	Version string
}

// Fetch reads the spec the target instance serves.
func Fetch(ctx context.Context, c *client.Client) (*Spec, error) {
	req, err := mgmt.NewGetSwaggerJsonRequest(c.Endpoint())
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.Do(ctx, req, &raw); err != nil {
		return nil, fmt.Errorf("read the target Logto's own API spec: %w", err)
	}
	return Parse(raw)
}

// Parse loads a spec document and recognises its version. A document already parsed in this process (same bytes)
// is not parsed again: a Spec is read-only, and loading Logto's (some 800 KB) is the costly part of a run's setup.
func Parse(raw []byte) (*Spec, error) {
	key := sha256.Sum256(raw)
	if s, ok := parsed.Load(key); ok {
		return s.(*Spec), nil
	}
	d, err := openapi.Load(raw)
	if err != nil {
		return nil, err
	}
	v, err := logtospecs.Match(raw)
	if err != nil {
		return nil, err
	}
	s, _ := parsed.LoadOrStore(key, &Spec{doc: d, Version: v})
	return s.(*Spec), nil
}

var parsed sync.Map // sha256 of a raw document -> *Spec

// Need is what one declared object will send: an operation and the property paths of its body (a.b.c), with where
// they come from in the document, for the message.
type Need struct {
	OperationID string
	Fields      []string
	Where       string // e.g. spec.signInExperience
}

// Check returns every need the target does not support, as one error.
func (s *Spec) Check(needs ...Need) error {
	var errs []error
	for _, n := range needs {
		if !s.doc.Has(n.OperationID) {
			errs = append(errs, fmt.Errorf("%s: this Logto has no %s operation (absent from its own API spec)", n.Where, n.OperationID))
			continue
		}
		schema, err := s.doc.RequestSchema(n.OperationID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, f := range n.Fields {
			if schema == nil || !openapi.HasPath(schema, f) {
				errs = append(errs, fmt.Errorf("%s.%s: this Logto does not accept %q in %s (absent from its own API spec)",
					n.Where, f, f, n.OperationID))
			}
		}
	}
	return errors.Join(errs...)
}

// Fields lists the property paths of a wire value as it will be sent (only what marshals), objects traversed, arrays
// looked into.
func Fields(body any) ([]string, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	var out []string
	var walk func(any, string)
	walk = func(n any, path string) {
		switch x := n.(type) {
		case map[string]any:
			for k, c := range x {
				p := k
				if path != "" {
					p = path + "." + k
				}
				out = append(out, p)
				walk(c, p)
			}
		case []any:
			for _, c := range x {
				walk(c, path)
			}
		}
	}
	walk(v, "")
	return dedupe(out), nil
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// Free-form objects (Logto passes them as is) have no declared properties below them; callers stop their fields at
// such a key with Prefix.
func Prefix(fields []string, prefix string) []string {
	var out []string
	for _, f := range fields {
		if f == prefix || !strings.HasPrefix(f, prefix+".") {
			out = append(out, f)
		}
	}
	return out
}
