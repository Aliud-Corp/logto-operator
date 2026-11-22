// Package kindkit holds what every kind needs: listing through the generated builders, JSON comparison that
// tolerates fields Logto adds, and the change helpers of a plan.
package kindkit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"

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

// Deletes is the need of a delete operation (no body: only its presence is checked), when the kind's prune switch
// is on.
func Deletes(on bool, where string, operationIDs ...string) []capability.Need {
	if !on {
		return nil
	}
	out := make([]capability.Need, 0, len(operationIDs))
	for _, op := range operationIDs {
		out = append(out, capability.Need{OperationID: op, Where: where})
	}
	return out
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
		// Decoded JSON scalars: a number, a string, a boolean or null; a type change is a difference.
		return reflect.DeepEqual(w, h)
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

// Overlay is what Logto holds (raw, as decoded from its answer) with the declared value written over it: objects are
// merged key by key, recursively; anything else (a list, a scalar) is the declared value. Kinds send it where Logto
// replaces a sub-object whole, so that a field they do not manage (set in the console, or added by a newer Logto)
// survives.
func Overlay(have, want any) any {
	w := normalize(want)
	h, ok := normalize(have).(map[string]any)
	wm, isObject := w.(map[string]any)
	if !ok || !isObject {
		return w
	}
	out := make(map[string]any, len(h)+len(wm))
	for k, v := range h {
		out[k] = v
	}
	for k, v := range wm {
		out[k] = Overlay(h[k], v)
	}
	return out
}

// OverlayBy is Overlay for a list of objects that Logto replaces whole: each declared item is written over the item
// Logto holds with the same key field (a sign-in method by its identifier), so that fields a kind does not manage
// survive on each item. The list itself (order, items) is the declared one.
func OverlayBy(key string, have, want any) any {
	held := map[string]any{}
	if list, ok := normalize(have).([]any); ok {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok {
				held[fmt.Sprint(m[key])] = m
			}
		}
	}
	w, ok := normalize(want).([]any)
	if !ok {
		return normalize(want)
	}
	out := make([]any, len(w))
	for i, item := range w {
		m, isObject := item.(map[string]any)
		if !isObject {
			out[i] = item
			continue
		}
		out[i] = Overlay(held[fmt.Sprint(m[key])], m)
	}
	return out
}

// Equal reports whether two values are the same once encoded as JSON (key order aside, types included).
func Equal(a, b any) bool { return reflect.DeepEqual(normalize(a), normalize(b)) }

// InSync reports whether writing want over what Logto holds (Overlay) would change nothing: a declared value is in
// sync even when Logto holds more next to it.
func InSync(have, want any) bool { return Equal(Overlay(have, want), have) }

// Deref is the string a pointer holds, empty for nil.
func Deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

// ManagementAPI reports whether a resource indicator is Logto's own Management API: its resource, and any role
// holding one of its scopes, is never deleted or stripped, or the tool would cut its own access.
func ManagementAPI(indicator string) bool {
	return strings.HasPrefix(indicator, "https://default.logto.app")
}

// Kept reports whether an object is protected from prune by spec.prune.keep, given the keys it can be listed by:
// its name, its indicator for a resource, its id for a connector.
func Kept(env *engine.Env, keys ...string) bool {
	for _, k := range env.Spec.Prune.Keep {
		for _, key := range keys {
			if key != "" && k == key {
				return true
			}
		}
	}
	return false
}
