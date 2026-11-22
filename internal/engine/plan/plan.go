// Package plan is what an engine run intends to do: per kind, which objects are created, updated (field by field),
// deleted or left unchanged. It renders like a diff, in a stable order, and never shows a secret.
package plan

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Action of one op.
type Action int

const (
	Noop Action = iota
	Create
	Update
	Delete
)

func (a Action) symbol() string { return [...]string{"=", "+", "~", "-"}[a] }

// KnownAfterApply is the value of a field Logto generates (an id) for an object the same plan creates.
const KnownAfterApply = "(known after apply)"

// Masked is how a sensitive value renders.
const Masked = "(sensitive)"

// Change is one field of an op.
type Change struct {
	Field     string
	From, To  string // empty From on a create, empty To on a delete
	Sensitive bool
}

// Result is what became of an op during an apply.
type Result int

const (
	NotRun  Result = iota // planned, never run (a plan, a no-op, or an op after a failure)
	Applied               // written, and kept by Logto
	Failed                // the write failed
)

func (r Result) String() string { return [...]string{"not run", "applied", "failed"}[r] }

// Op is what happens to one declared (or, on prune, undeclared) object.
type Op struct {
	Kind    string
	Key     string // local key in the document, or the Logto name on prune
	Name    string // Logto name, for the reader
	Action  Action
	Changes []Change
	Result  Result // set by an apply
	// Payload is the kind's own data for Apply (its wire body, the Logto id): opaque to the engine, and never
	// encoded nor printed, since a body may hold a secret (Format).
	Payload any `json:"-"`
}

// Format prints an op without its payload, for every verb (%v, %+v, %#v, %s): a payload may hold a secret.
func (op Op) Format(f fmt.State, verb rune) {
	payload := ""
	if op.Payload != nil {
		payload = " payload:(hidden)"
	}
	_, _ = fmt.Fprintf(f, "{%s %s %q %s %d change(s) %s%s}", op.Kind, op.Key, op.Name, op.Action.symbol(), len(op.Changes), op.Result, payload)
}

// Plan is the ops of a run, in the engine's kind order.
type Plan struct {
	Kinds []string // kinds in reconciliation order, also the ones that planned nothing
	Ops   []Op
}

// Add appends ops, registering their kind. A sensitive change is masked on the way in: no value of it is kept.
func (p *Plan) Add(kind string, ops ...Op) {
	if !contains(p.Kinds, kind) {
		p.Kinds = append(p.Kinds, kind)
	}
	for _, op := range ops {
		changes := make([]Change, len(op.Changes))
		for i, c := range op.Changes {
			if c.Sensitive {
				c.From, c.To = mask(c.From), mask(c.To)
			}
			changes[i] = c
		}
		op.Changes = changes
		p.Ops = append(p.Ops, op)
	}
}

func mask(s string) string {
	if s == "" {
		return ""
	}
	return Masked
}

// Changed reports whether the plan writes anything.
func (p *Plan) Changed() bool {
	for _, op := range p.Ops {
		if op.Action != Noop {
			return true
		}
	}
	return false
}

// Count returns how many ops have an action.
func (p *Plan) Count(a Action) int {
	n := 0
	for _, op := range p.Ops {
		if op.Action == a {
			n++
		}
	}
	return n
}

// Render writes the plan: one block per kind with its creates, updates and deletes, then an unchanged count, then a
// summary line.
func Render(w io.Writer, p *Plan) error {
	var b strings.Builder
	for _, kind := range p.Kinds {
		var ops []Op
		unchanged := 0
		for _, op := range p.Ops {
			if op.Kind != kind {
				continue
			}
			if op.Action == Noop {
				unchanged++
				continue
			}
			ops = append(ops, op)
		}
		sort.SliceStable(ops, func(i, j int) bool { return ops[i].Key < ops[j].Key })
		fmt.Fprintf(&b, "%s\n", kind)
		for _, op := range ops {
			label := op.Key
			if op.Name != "" && op.Name != op.Key {
				label += " (" + op.Name + ")"
			}
			fmt.Fprintf(&b, "  %s %s\n", op.Action.symbol(), label)
			for _, c := range op.Changes {
				fmt.Fprintf(&b, "      %s\n", renderChange(op.Action, c))
			}
		}
		if unchanged > 0 {
			fmt.Fprintf(&b, "  = %d unchanged\n", unchanged)
		}
	}
	fmt.Fprintf(&b, "Plan: %d to create, %d to update, %d to delete, %d unchanged.\n",
		p.Count(Create), p.Count(Update), p.Count(Delete), p.Count(Noop))
	_, err := io.WriteString(w, b.String())
	return err
}

func renderChange(a Action, c Change) string {
	from, to := c.From, c.To
	if c.Sensitive {
		from, to = mask(from), mask(to)
	}
	switch a {
	case Create:
		return c.Field + ": " + to
	case Delete:
		return c.Field + ": " + from
	default:
		return c.Field + ": " + orNone(from) + " -> " + orNone(to)
	}
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// SameSet reports whether two string lists hold the same elements, in any order (duplicates ignored).
func SameSet(a, b []string) bool {
	set := func(l []string) map[string]bool {
		m := map[string]bool{}
		for _, s := range l {
			m[s] = true
		}
		return m
	}
	sa, sb := set(a), set(b)
	if len(sa) != len(sb) {
		return false
	}
	for k := range sa {
		if !sb[k] {
			return false
		}
	}
	return true
}

// List renders a string list for a Change, sorted.
func List(l []string) string {
	c := append([]string(nil), l...)
	sort.Strings(c)
	return "[" + strings.Join(c, ", ") + "]"
}
