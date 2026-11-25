package applications

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// consentChange is what an application's consent scopes need: scopes to add (resource scopes by reference, resolved
// to ids at apply, as a resource this run creates has none yet) and scopes to remove.
type consentChange struct {
	addResources []v1alpha1.ResourceScopeRef
	addUser      []string
	rmResources  []string // scope ids
	rmUser       []string
}

func (c consentChange) empty() bool {
	return len(c.addResources) == 0 && len(c.addUser) == 0 && len(c.rmResources) == 0 && len(c.rmUser) == 0
}

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

// consentCreate is the consent of an application this run creates: everything declared is added.
func consentCreate(env *engine.Env, d v1alpha1.Application) (consentChange, []plan.Change) {
	var c consentChange
	var changes []plan.Change
	if cs := d.ConsentScopes; cs != nil {
		if cs.Resources != nil && len(*cs.Resources) > 0 {
			c.addResources = *cs.Resources
			changes = append(changes, plan.Change{Field: "consentScopes.resources", To: plan.List(wantedResources(env, *cs.Resources))})
		}
		if cs.User != nil && len(*cs.User) > 0 {
			c.addUser = wantedUser(*cs.User)
			changes = append(changes, plan.Change{Field: "consentScopes.user", To: plan.List(c.addUser)})
		}
	}
	return c, changes
}

// consentUpdate compares the declared consent scopes with what the application holds: a declared list is exact.
func consentUpdate(ctx context.Context, env *engine.Env, d v1alpha1.Application, id string) (consentChange, []plan.Change, error) {
	var c consentChange
	cs := d.ConsentScopes
	if cs == nil || (cs.Resources == nil && cs.User == nil) {
		return c, nil, nil
	}
	req, err := mgmt.NewListApplicationUserConsentScopesRequest(env.Client.Endpoint(), id)
	if err != nil {
		return c, nil, err
	}
	var held wireConsent
	if err := env.Client.Do(ctx, req, &held); err != nil {
		return c, nil, err
	}
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
				c.addResources = append(c.addResources, ref)
			}
		}
		for k, sid := range have {
			if !want[k] {
				c.rmResources = append(c.rmResources, sid)
			}
		}
		sort.Strings(c.rmResources)
		if len(c.addResources) > 0 || len(c.rmResources) > 0 {
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
				c.addUser = append(c.addUser, u)
			}
		}
		for _, u := range held.UserScopes {
			if !want[u] {
				c.rmUser = append(c.rmUser, u)
			}
		}
		sort.Strings(c.rmUser)
		if len(c.addUser) > 0 || len(c.rmUser) > 0 {
			from := append([]string{}, held.UserScopes...)
			sort.Strings(from)
			changes = append(changes, plan.Change{Field: "consentScopes.user", From: plan.List(from), To: plan.List(wantedUser(*cs.User))})
		}
	}
	return c, changes, nil
}

// applyConsent adds, then removes, an application's consent scopes. The resource scope ids are known once the
// resources kind has applied.
func applyConsent(ctx context.Context, env *engine.Env, id string, c consentChange) error {
	if len(c.addResources) > 0 || len(c.addUser) > 0 {
		ids := []string{}
		for _, ref := range c.addResources {
			sid, ok := env.Refs.Get(resources.Name, ref.Resource, resources.ScopeRef(ref.Scope))
			if !ok {
				return fmt.Errorf("scope %q of resource %q has no id yet", ref.Scope, ref.Resource)
			}
			ids = append(ids, sid)
		}
		body := addConsent{ResourceScopes: ids, UserScopes: c.addUser}
		if err := kindkit.Send(ctx, env, body, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateApplicationUserConsentScopeRequestWithBody(s, id, ct, b)
		}); err != nil {
			return err
		}
	}
	remove := func(scopeType mgmt.DeleteApplicationUserConsentScopeParamsScopeType, scopeID string) error {
		req, err := mgmt.NewDeleteApplicationUserConsentScopeRequest(env.Client.Endpoint(), id, scopeType, scopeID)
		if err != nil {
			return err
		}
		return env.Client.Do(ctx, req, nil)
	}
	for _, sid := range c.rmResources {
		if err := remove("resource-scopes", sid); err != nil {
			return err
		}
	}
	for _, u := range c.rmUser {
		if err := remove("user-scopes", u); err != nil {
			return err
		}
	}
	return nil
}
