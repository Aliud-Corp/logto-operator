package roles

import (
	"fmt"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
)

// Held is a role Logto holds, as a granting kind lists it.
type Held struct{ ID, Type string }

// Grant is a role a kind grants (to an application, a user): by its id when Logto holds it, or by its document key
// when this run creates it (the id is learnt during the apply, from Refs).
type Grant struct {
	Name, ID, Key string
}

// Resolve turns the roles a declaration grants into grants. A role the document declares comes from Refs (no id yet:
// this run creates it); any other must be held by Logto, with the type the grantee takes.
func Resolve(env *engine.Env, path string, refs []v1alpha1.RoleRef, held map[string]Held, typ string) ([]Grant, error) {
	var out []Grant
	for i, ref := range refs {
		if key, _, ok := Declared(env.Spec, ref.Name); ok {
			id, _ := env.Refs.Get(Name, key, RefID)
			out = append(out, Grant{Name: ref.Name, ID: id, Key: key})
			continue
		}
		h, ok := held[ref.Name]
		if !ok {
			return nil, fmt.Errorf("%s.roles[%d]: Logto has no %s role named %q, and spec.roles declares none", path, i, typ, ref.Name)
		}
		if h.Type != typ {
			return nil, fmt.Errorf("%s.roles[%d]: %q is a %s role; a %s role is needed here", path, i, ref.Name, h.Type, typ)
		}
		out = append(out, Grant{Name: ref.Name, ID: h.ID})
	}
	return out, nil
}

// IDs are the grants' role ids, during an apply: a role this run created has its id in Refs by now.
func IDs(env *engine.Env, grants []Grant) ([]string, error) {
	out := make([]string, 0, len(grants))
	for _, g := range grants {
		id := g.ID
		if id == "" {
			var ok bool
			if id, ok = env.Refs.Get(Name, g.Key, RefID); !ok {
				return nil, fmt.Errorf("role %q has no id yet", g.Name)
			}
		}
		out = append(out, id)
	}
	return out, nil
}
