// Package kindkit holds what every kind needs: listing through the generated builders, JSON comparison that
// tolerates fields Logto adds, and the change helpers of a plan.
package kindkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
)

// Page is a page and its size, as the generated list builders take them.
type Page struct{ Page, Size *int }

// List reads every page of a list built by a generated builder taking the page parameters.
func List[T any](ctx context.Context, env *engine.Env, build func(Page) (*http.Request, error)) ([]T, error) {
	return client.List[T](ctx, env.Client, func(page, size int) (*http.Request, error) {
		return build(Page{Page: &page, Size: &size})
	})
}

// Send encodes a wire body and sends it with a generated builder taking (server, contentType, body).
func Send(ctx context.Context, env *engine.Env, body any, out any, build func(server, contentType string, body *bytes.Reader) (*http.Request, error)) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := build(env.Client.Endpoint(), "application/json", bytes.NewReader(b))
	if err != nil {
		return err
	}
	return env.Client.Do(ctx, req, out)
}

// Need builds the capability need of a wire body: the fields it will send, under where.
func Need(operationID, where string, body any) (capability.Need, error) {
	fields, err := capability.Fields(body)
	if err != nil {
		return capability.Need{}, err
	}
	return capability.Need{OperationID: operationID, Fields: fields, Where: where}, nil
}

// Subset reports whether every value of want is in have, recursively: Logto adds fields and defaults to what it
// stores, which must not look like drift. Arrays are compared element by element (same length).
func Subset(want, have any) bool {
	w, h := normalize(want), normalize(have)
	return subset(w, h)
}

func subset(w, h any) bool {
	switch wv := w.(type) {
	case map[string]any:
		hv, ok := h.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range wv {
			if !subset(v, hv[k]) {
				return false
			}
		}
		return true
	case []any:
		hv, ok := h.([]any)
		if !ok || len(hv) != len(wv) {
			return false
		}
		for i := range wv {
			if !subset(wv[i], hv[i]) {
				return false
			}
		}
		return true
	default:
		return fmt.Sprint(w) == fmt.Sprint(h)
	}
}

func normalize(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

// JSON renders a value compactly for a plan change.
func JSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// Field appends a change when two rendered values differ.
func Field(changes []plan.Change, field, have, want string) []plan.Change {
	if have == want {
		return changes
	}
	return append(changes, plan.Change{Field: field, From: have, To: want})
}

// Action is Update when there are changes, Noop otherwise.
func Action(changes []plan.Change) plan.Action {
	if len(changes) > 0 {
		return plan.Update
	}
	return plan.Noop
}

// Keys returns the sorted keys of a map.
func Keys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Kept reports whether a Logto name is protected from prune by spec.prune.keep.
func Kept(env *engine.Env, name string) bool {
	for _, k := range env.Spec.Prune.Keep {
		if k == name {
			return true
		}
	}
	return false
}
