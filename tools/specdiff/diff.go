package main

import (
	"fmt"
	"sort"
	"strings"
)

// Level classifies a difference between two versions of the spec.
type Level int

const (
	// Additive changes cannot break a client of the older version: a new operation, an optional field, an enum value.
	Additive Level = iota
	// Changed differences need a look but break nothing by themselves (a oneOf gaining a branch, a body appearing).
	Changed
	// Breaking changes can break a client: removed operation or field, new required input, type change, enum value
	// removed.
	Breaking
)

func (l Level) String() string {
	return [...]string{"ADDITIVE", "CHANGED", "BREAKING"}[l]
}

// Finding is one difference, attached to the operation it was found in.
type Finding struct {
	Level       Level
	OperationID string // of the operation in the older spec (the name the code calls), in the newer one when added
	Where       string
	What        string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s: %s", f.Where, f.What)
}

var methods = []string{"get", "post", "put", "patch", "delete"}

type differ struct {
	from, to map[string]any
	out      []Finding
	op       string
}

func (d *differ) add(l Level, where, what string) {
	d.out = append(d.out, Finding{Level: l, OperationID: d.op, Where: where, What: what})
}

// Diff compares two OpenAPI documents operation by operation.
func Diff(from, to map[string]any) []Finding {
	d := &differ{from: from, to: to}
	opsFrom, opsTo := operations(from), operations(to)
	for _, k := range sortedKeys(opsFrom) {
		if _, ok := opsTo[k]; !ok {
			d.op = opID(opsFrom[k].op)
			d.add(Breaking, k, "operation removed")
		}
	}
	for _, k := range sortedKeys(opsTo) {
		if _, ok := opsFrom[k]; !ok {
			d.op = opID(opsTo[k].op)
			d.add(Additive, k, "operation added ("+d.op+")")
		}
	}
	for _, k := range sortedKeys(opsFrom) {
		b, ok := opsTo[k]
		if !ok {
			continue
		}
		a := opsFrom[k]
		// The code calls the operation by its older name (mgmt.New<OperationId>Request): findings stay under it, and
		// a rename is breaking, since that generated method disappears.
		d.op = opID(a.op)
		if opID(a.op) != opID(b.op) {
			d.add(Breaking, k, fmt.Sprintf("operationId %s -> %s (generated method renamed)", opID(a.op), opID(b.op)))
		}
		d.params(k, a, b)
		d.body(k, a.op, b.op)
		d.responses(k, a.op, b.op)
	}
	return d.out
}

type operation struct {
	item map[string]any
	op   map[string]any
}

func operations(doc map[string]any) map[string]operation {
	out := map[string]operation{}
	paths, _ := doc["paths"].(map[string]any)
	for path, raw := range paths {
		item, _ := raw.(map[string]any)
		for _, m := range methods {
			if op, ok := item[m].(map[string]any); ok {
				out[strings.ToUpper(m)+" "+path] = operation{item: item, op: op}
			}
		}
	}
	return out
}

func opID(op map[string]any) string {
	s, _ := op["operationId"].(string)
	return s
}

func resolve(doc map[string]any, node any) map[string]any {
	n, _ := node.(map[string]any)
	for i := 0; i < 20 && n != nil; i++ {
		ref, ok := n["$ref"].(string)
		if !ok {
			return n
		}
		var cur any = doc
		for _, part := range strings.Split(strings.TrimPrefix(ref, "#/"), "/") {
			m, _ := cur.(map[string]any)
			cur = m[part]
		}
		n, _ = cur.(map[string]any)
	}
	return n
}

func (d *differ) params(where string, a, b operation) {
	pa, pb := d.paramMap(d.from, a), d.paramMap(d.to, b)
	for _, k := range sortedKeys(pa) {
		if _, ok := pb[k]; !ok {
			d.add(Breaking, where, "parameter "+k+" removed")
		}
	}
	for _, k := range sortedKeys(pb) {
		if _, ok := pa[k]; ok {
			continue
		}
		if req, _ := pb[k]["required"].(bool); req {
			d.add(Breaking, where, "parameter "+k+" added (required)")
		} else {
			d.add(Additive, where, "parameter "+k+" added")
		}
	}
}

func (d *differ) paramMap(doc map[string]any, o operation) map[string]map[string]any {
	out := map[string]map[string]any{}
	var list []any
	if l, ok := o.item["parameters"].([]any); ok {
		list = append(list, l...)
	}
	if l, ok := o.op["parameters"].([]any); ok {
		list = append(list, l...)
	}
	for _, p := range list {
		r := resolve(doc, p)
		in, _ := r["in"].(string)
		name, _ := r["name"].(string)
		out[in+":"+name] = r
	}
	return out
}

func jsonSchema(doc map[string]any, holder map[string]any) map[string]any {
	content, _ := resolve(doc, holder)["content"].(map[string]any)
	media, _ := content["application/json"].(map[string]any)
	if media == nil {
		return nil
	}
	s, _ := media["schema"].(map[string]any)
	return s
}

func (d *differ) body(where string, a, b map[string]any) {
	ba, bb := jsonSchema(d.from, asMap(a["requestBody"])), jsonSchema(d.to, asMap(b["requestBody"]))
	switch {
	case ba != nil && bb != nil:
		d.schema(ba, bb, where+" request", true)
	case ba != nil:
		d.add(Breaking, where, "request body removed")
	case bb != nil:
		d.add(Changed, where, "request body added")
	}
}

func (d *differ) responses(where string, a, b map[string]any) {
	ra, rb := asMap(a["responses"]), asMap(b["responses"])
	for _, code := range sortedKeys(ra) {
		if _, ok := rb[code]; !ok {
			d.add(Changed, where, "response "+code+" removed")
		}
	}
	for _, code := range sortedKeys(rb) {
		if _, ok := ra[code]; !ok {
			d.add(Additive, where, "response "+code+" added")
		}
	}
	for _, code := range sortedKeys(ra) {
		if _, ok := rb[code]; !ok {
			continue
		}
		x, y := jsonSchema(d.from, asMap(ra[code])), jsonSchema(d.to, asMap(rb[code]))
		if x != nil && y != nil {
			d.schema(x, y, where+" response "+code, false)
		}
	}
}

func kind(s map[string]any) string {
	for _, k := range []string{"oneOf", "anyOf", "allOf"} {
		if _, ok := s[k]; ok {
			return k
		}
	}
	t, _ := s["type"].(string)
	return t
}

func (d *differ) schema(rawA, rawB map[string]any, where string, request bool) {
	a, b := resolve(d.from, rawA), resolve(d.to, rawB)
	if a == nil || b == nil {
		return
	}
	ka, kb := kind(a), kind(b)
	if ka != kb {
		d.add(Breaking, where, fmt.Sprintf("type %s -> %s", ka, kb))
		return
	}
	na, _ := a["nullable"].(bool)
	nb, _ := b["nullable"].(bool)
	if na != nb {
		if !request && nb {
			d.add(Breaking, where, "may now be null")
		} else {
			d.add(Additive, where, fmt.Sprintf("nullable %v -> %v", na, nb))
		}
	}
	ea, eb := enumSet(a), enumSet(b)
	if removed := minus(ea, eb); len(removed) > 0 {
		d.add(Breaking, where, "enum values removed "+strings.Join(removed, ", "))
	}
	if added := minus(eb, ea); len(added) > 0 {
		d.add(Additive, where, "enum values added "+strings.Join(added, ", "))
	}
	switch ka {
	case "oneOf", "anyOf", "allOf":
		la, _ := a[ka].([]any)
		lb, _ := b[ka].([]any)
		if len(la) != len(lb) {
			d.add(Changed, where, fmt.Sprintf("%s %d -> %d branches", ka, len(la), len(lb)))
		}
		for i := 0; i < len(la) && i < len(lb); i++ {
			d.schema(asMap(la[i]), asMap(lb[i]), fmt.Sprintf("%s.%s[%d]", where, ka, i), request)
		}
		return
	case "array":
		d.schema(asMap(a["items"]), asMap(b["items"]), where+"[]", request)
		return
	}
	pa, pb := asMap(a["properties"]), asMap(b["properties"])
	reqA, reqB := stringSet(a["required"]), stringSet(b["required"])
	for _, k := range sortedKeys(pa) {
		if _, ok := pb[k]; !ok {
			d.add(Breaking, where+"."+k, "property removed")
		}
	}
	for _, k := range sortedKeys(pb) {
		if _, ok := pa[k]; ok {
			continue
		}
		if request && reqB[k] {
			d.add(Breaking, where+"."+k, "new required request property")
		} else {
			d.add(Additive, where+"."+k, "property added")
		}
	}
	for _, k := range sortedKeys(pa) {
		if _, ok := pb[k]; !ok {
			continue
		}
		if request && reqB[k] && !reqA[k] {
			d.add(Breaking, where+"."+k, "became required")
		}
		if !request && reqA[k] && !reqB[k] {
			d.add(Breaking, where+"."+k, "no longer always returned")
		}
		d.schema(asMap(pa[k]), asMap(pb[k]), where+"."+k, request)
	}
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func enumSet(s map[string]any) map[string]bool {
	out := map[string]bool{}
	if l, ok := s["enum"].([]any); ok {
		for _, v := range l {
			out[fmt.Sprint(v)] = true
		}
	}
	return out
}

func stringSet(v any) map[string]bool {
	out := map[string]bool{}
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

func minus(a, b map[string]bool) []string {
	var out []string
	for k := range a {
		if !b[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
