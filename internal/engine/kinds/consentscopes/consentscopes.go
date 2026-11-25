// Package consentscopes is the diff and the apply of a consent screen's scopes, shared by the kinds that own one: a
// third-party application, and the tenant-wide consent ceiling of Client ID Metadata Document clients. Logto answers
// and accepts the same shapes for both; only the routes differ, so the kind supplies the requests.
package consentscopes

import (
	"fmt"
	"sort"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
)

// Change is what a consent screen needs: scopes to add (resource scopes by reference, resolved to ids at apply, as a
// resource this run creates has none yet) and scopes to remove.
type Change struct {
	AddResources []v1alpha1.ResourceScopeRef
	AddUser      []string
	RmResources  []string // scope ids
	RmUser       []string
}

// Empty reports whether nothing needs to change.
func (c Change) Empty() bool {
	return len(c.AddResources) == 0 && len(c.AddUser) == 0 && len(c.RmResources) == 0 && len(c.RmUser) == 0
}

// Held is what a consent screen holds, as Logto lists it. Organization scopes are not managed: they are not read.
type Held struct {
	ResourceScopes []struct {
		Resource struct {
			Indicator string `json:"indicator"`
		} `json:"resource"`
		Scopes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"scopes"`
	} `json:"resourceScopes"`
	UserScopes []string `json:"userScopes"`
}

// Add is the body that adds consent scopes; a list that is empty is not sent.
type Add struct {
	ResourceScopes []string `json:"resourceScopes,omitempty"`
	UserScopes     []string `json:"userScopes,omitempty"`
}

// Scope types of the delete route, as Logto spells them.
const (
	TypeResourceScopes = "resource-scopes"
	TypeUserScopes     = "user-scopes"
)

// scopeKey is how a resource scope is compared: its resource's indicator and its name.
func scopeKey(indicator, name string) string { return indicator + " " + name }

func wantedResources(env *engine.Env, refs []v1alpha1.ResourceScopeRef) []string {
	out := []string{}
	for _, ref := range refs {
		out = append(out, scopeKey(env.Spec.Resources[ref.Resource].Indicator, ref.Scope))
	}
	sort.Strings(out)
	return out
}

func wantedUser(scopes []v1alpha1.ConsentUserScope) []string {
	out := []string{}
	for _, u := range scopes {
		out = append(out, string(u))
	}
	sort.Strings(out)
	return out
}

// Declared reports whether the block names at least one list, so there is something to read and compare.
func Declared(cs *v1alpha1.ConsentScopes) bool {
	return cs != nil && (cs.Resources != nil || cs.User != nil)
}

// Create is the consent of a screen that starts empty: everything declared is added.
func Create(env *engine.Env, cs *v1alpha1.ConsentScopes) (Change, []plan.Change) {
	var c Change
	var changes []plan.Change
	if cs != nil {
		if cs.Resources != nil && len(*cs.Resources) > 0 {
			c.AddResources = *cs.Resources
			changes = append(changes, plan.Change{Field: "consentScopes.resources", To: plan.List(wantedResources(env, *cs.Resources))})
		}
		if cs.User != nil && len(*cs.User) > 0 {
			c.AddUser = wantedUser(*cs.User)
			changes = append(changes, plan.Change{Field: "consentScopes.user", To: plan.List(c.AddUser)})
		}
	}
	return c, changes
}

// Diff compares the declared consent scopes with what the screen holds: a declared list is exact.
func Diff(env *engine.Env, cs *v1alpha1.ConsentScopes, held Held) (Change, []plan.Change) {
	var c Change
	var changes []plan.Change
	if cs.Resources != nil {
		have := map[string]string{} // key -> scope id
		var from []string
		for _, g := range held.ResourceScopes {
			for _, s := range g.Scopes {
				k := scopeKey(g.Resource.Indicator, s.Name)
				have[k] = s.ID
				from = append(from, k)
			}
		}
		want := map[string]bool{}
		for _, ref := range *cs.Resources {
			k := scopeKey(env.Spec.Resources[ref.Resource].Indicator, ref.Scope)
			want[k] = true
			if _, ok := have[k]; !ok {
				c.AddResources = append(c.AddResources, ref)
			}
		}
		for k, sid := range have {
			if !want[k] {
				c.RmResources = append(c.RmResources, sid)
			}
		}
		sort.Strings(c.RmResources)
		if len(c.AddResources) > 0 || len(c.RmResources) > 0 {
			sort.Strings(from)
			changes = append(changes, plan.Change{Field: "consentScopes.resources", From: plan.List(from), To: plan.List(wantedResources(env, *cs.Resources))})
		}
	}
	if cs.User != nil {
		have := map[string]bool{}
		for _, u := range held.UserScopes {
			have[u] = true
		}
		want := map[string]bool{}
		for _, u := range wantedUser(*cs.User) {
			want[u] = true
			if !have[u] {
				c.AddUser = append(c.AddUser, u)
			}
		}
		for _, u := range held.UserScopes {
			if !want[u] {
				c.RmUser = append(c.RmUser, u)
			}
		}
		sort.Strings(c.RmUser)
		if len(c.AddUser) > 0 || len(c.RmUser) > 0 {
			from := append([]string{}, held.UserScopes...)
			sort.Strings(from)
			changes = append(changes, plan.Change{Field: "consentScopes.user", From: plan.List(from), To: plan.List(wantedUser(*cs.User))})
		}
	}
	return c, changes
}

// Apply adds, then removes, the scopes of a consent screen. The resource scope ids are known once the resources kind
// has applied. The kind sends the additions and the deletions, since the routes are its own.
func Apply(env *engine.Env, c Change, add func(Add) error, remove func(scopeType, id string) error) error {
	if len(c.AddResources) > 0 || len(c.AddUser) > 0 {
		ids := []string{}
		for _, ref := range c.AddResources {
			sid, ok := env.Refs.Get(resources.Name, ref.Resource, resources.ScopeRef(ref.Scope))
			if !ok {
				return fmt.Errorf("scope %q of resource %q has no id yet", ref.Scope, ref.Resource)
			}
			ids = append(ids, sid)
		}
		if err := add(Add{ResourceScopes: ids, UserScopes: c.AddUser}); err != nil {
			return err
		}
	}
	for _, sid := range c.RmResources {
		if err := remove(TypeResourceScopes, sid); err != nil {
			return err
		}
	}
	for _, u := range c.RmUser {
		if err := remove(TypeUserScopes, u); err != nil {
			return err
		}
	}
	return nil
}
