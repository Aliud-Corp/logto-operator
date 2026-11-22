package kindkit

import (
	"fmt"

	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
)

// Collection plans a family of Logto objects matched by a natural key (a name, an indicator): each declared object
// is created when Logto holds none with its key, updated when it holds exactly one, and refused when it holds
// several (adopting one of them would be a guess); with Prune, every held object that no declaration matches is
// deleted, unless it is Protected or listed in Keep. Kinds supply what differs: the keys and the ops.
//
// D is the declared type, H the held (wire) type. The ops come out declared ones first, in key order, then the
// deletes in the order Logto listed the objects.
type Collection[D, H any] struct {
	Kind     string       // the kind's name
	Where    string       // the document path of the declarations ("spec.organizationScopes")
	Declared map[string]D // by document key

	Natural func(key string, d D) string // the natural key of a declaration
	Held    func(h H) string             // the natural key of a held object

	Prune     bool
	Keep      []string           // spec.prune.keep
	KeepKeys  func(h H) []string // the keys Keep may name an object by; the natural key when nil
	Protected func(h H) bool     // never pruned (Logto's own objects); none when nil

	Create func(key string, d D) ([]plan.Op, error)
	Update func(key string, d D, have H) ([]plan.Op, error)
	Delete func(h H) plan.Op
}

// Plan diffs the declarations with what Logto holds.
func (c Collection[D, H]) Plan(observed []H) ([]plan.Op, error) {
	held := map[string][]H{}
	for _, h := range observed {
		k := c.Held(h)
		held[k] = append(held[k], h)
	}
	declaredBy := map[string]string{} // natural key -> document key
	var ops []plan.Op
	for _, key := range Keys(c.Declared) {
		d := c.Declared[key]
		natural := c.Natural(key, d)
		path := c.Where + "." + key
		if first, ok := declaredBy[natural]; ok {
			return nil, fmt.Errorf("%s: declares %q like %s.%s", path, natural, c.Where, first)
		}
		declaredBy[natural] = key
		var next []plan.Op
		var err error
		switch found := held[natural]; len(found) {
		case 0:
			next, err = c.Create(key, d)
		case 1:
			next, err = c.Update(key, d, found[0])
		default:
			return nil, fmt.Errorf("%s: %d Logto objects match %q; rename or delete the extra ones so the match is unique", path, len(found), natural)
		}
		if err != nil {
			return nil, err
		}
		ops = append(ops, next...)
	}
	if !c.Prune {
		return ops, nil
	}
	for _, h := range observed {
		if _, ok := declaredBy[c.Held(h)]; ok || (c.Protected != nil && c.Protected(h)) || c.kept(h) {
			continue
		}
		ops = append(ops, c.Delete(h))
	}
	return ops, nil
}

func (c Collection[D, H]) kept(h H) bool {
	keys := []string{c.Held(h)}
	if c.KeepKeys != nil {
		keys = c.KeepKeys(h)
	}
	for _, k := range c.Keep {
		for _, key := range keys {
			if key != "" && k == key {
				return true
			}
		}
	}
	return false
}
