// Package organizations reconciles the organization template: organization scopes (by name) and organization roles
// (by name), each role holding exactly the organization scopes and resource scopes the document declares.
package organizations

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
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Kind names.
const (
	ScopesName = "organizationScopes"
	RolesName  = "organizationRoles"
)

// RefID is the ref field of an id.
const RefID = "id"

// Scopes reconciles spec.organizationScopes.
type Scopes struct{}

func (Scopes) Name() string        { return ScopesName }
func (Scopes) DependsOn() []string { return nil }

// Needs lists the bodies the kind can send.
func (Scopes) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, name := range kindkit.Keys(env.Spec.OrganizationScopes) {
		d := env.Spec.OrganizationScopes[name]
		where := "spec.organizationScopes." + name
		desc := d.Description
		for op, body := range map[string]any{"CreateOrganizationScope": newScope(name, d), "UpdateOrganizationScope": updateScope{Description: &desc}} {
			need, err := kindkit.Need(op, where, body)
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
	}
	return append(needs, kindkit.Deletes(env.Spec.Prune.OrganizationScopes, "spec.prune.organizationScopes", "DeleteOrganizationScope")...), nil
}

type scopePayload struct {
	id     string
	create *createScope
	update *updateScope
	delete bool
}

// Plan observes the organization scopes.
func (Scopes) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	observed, err := kindkit.List[wireScope](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListOrganizationScopesRequest(env.Client.Endpoint(), &mgmt.ListOrganizationScopesParams{Page: p.Page, PageSize: p.Size})
	})
	if err != nil {
		return nil, err
	}
	return kindkit.Collection[v1alpha1.OrganizationScope, wireScope]{
		Kind: ScopesName, Where: "spec.organizationScopes", Declared: env.Spec.OrganizationScopes,
		Natural: func(name string, _ v1alpha1.OrganizationScope) string { return name },
		Held:    func(s wireScope) string { return s.Name },
		Prune:   env.Spec.Prune.OrganizationScopes, Keep: env.Spec.Prune.Keep,
		Create: func(name string, d v1alpha1.OrganizationScope) ([]plan.Op, error) {
			return []plan.Op{{
				Kind: ScopesName, Key: name, Action: plan.Create,
				Changes: []plan.Change{{Field: "description", To: d.Description}},
				Payload: scopePayload{create: ptr(newScope(name, d))},
			}}, nil
		},
		Update: func(name string, d v1alpha1.OrganizationScope, have wireScope) ([]plan.Op, error) {
			env.Refs.Set(ScopesName, name, RefID, have.ID)
			var changes []plan.Change
			p := scopePayload{id: have.ID}
			if d.Description != "" {
				changes = kindkit.Field(changes, "description", kindkit.Deref(have.Description), d.Description)
			}
			if len(changes) > 0 {
				desc := d.Description
				p.update = &updateScope{Description: &desc}
			}
			return []plan.Op{{Kind: ScopesName, Key: name, Action: kindkit.Action(changes), Changes: changes, Payload: p}}, nil
		},
		Delete: func(s wireScope) plan.Op {
			return plan.Op{Kind: ScopesName, Key: s.Name, Action: plan.Delete, Payload: scopePayload{id: s.ID, delete: true}}
		},
	}.Plan(observed)
}

// Apply runs one op.
func (Scopes) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(scopePayload)
	switch {
	case p.delete:
		req, err := mgmt.NewDeleteOrganizationScopeRequest(env.Client.Endpoint(), p.id)
		if err != nil {
			return err
		}
		return env.Client.Do(ctx, req, nil)
	case p.create != nil:
		var created wireScope
		if err := kindkit.Send(ctx, env, p.create, &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateOrganizationScopeRequestWithBody(s, ct, b)
		}); err != nil {
			return err
		}
		env.Refs.Set(ScopesName, op.Key, RefID, created.ID)
		return nil
	case p.update != nil:
		return kindkit.Send(ctx, env, p.update, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateOrganizationScopeRequestWithBody(s, p.id, ct, b)
		})
	}
	return nil
}

// Roles reconciles spec.organizationRoles.
type Roles struct{}

func (Roles) Name() string        { return RolesName }
func (Roles) DependsOn() []string { return []string{ScopesName, resources.Name} }

// Needs lists the bodies the kind can send.
func (Roles) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, key := range kindkit.Keys(env.Spec.OrganizationRoles) {
		d := env.Spec.OrganizationRoles[key]
		where := "spec.organizationRoles." + key
		for _, n := range []struct {
			op   string
			body any
		}{
			{"CreateOrganizationRole", newRole(d, []string{}, []string{})},
			{"UpdateOrganizationRole", updateRole{Description: d.Description}},
			{"ReplaceOrganizationRoleScopes", replaceScopes{OrganizationScopeIDs: []string{}}},
			{"ReplaceOrganizationRoleResourceScopes", replaceResourceScopes{ScopeIDs: []string{}}},
		} {
			need, err := kindkit.Need(n.op, where, n.body)
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
	}
	return append(needs, kindkit.Deletes(env.Spec.Prune.OrganizationRoles, "spec.prune.organizationRoles", "DeleteOrganizationRole")...), nil
}

type rolePayload struct {
	id                          string
	d                           v1alpha1.OrganizationRole
	create, delete              bool
	description, scopes, rscope bool // what an update changes
}

// pair is a resource scope as the kind compares it: the resource's id and the scope's name.
type pair struct{ resourceID, scope string }

// Plan observes the organization roles.
func (Roles) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	observed, err := kindkit.List[wireRole](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListOrganizationRolesRequest(env.Client.Endpoint(), &mgmt.ListOrganizationRolesParams{Page: p.Page, PageSize: p.Size})
	})
	if err != nil {
		return nil, err
	}
	return kindkit.Collection[v1alpha1.OrganizationRole, wireRole]{
		Kind: RolesName, Where: "spec.organizationRoles", Declared: env.Spec.OrganizationRoles,
		Natural: func(_ string, d v1alpha1.OrganizationRole) string { return d.Name },
		Held:    func(r wireRole) string { return r.Name },
		Prune:   env.Spec.Prune.OrganizationRoles, Keep: env.Spec.Prune.Keep,
		Create: func(key string, d v1alpha1.OrganizationRole) ([]plan.Op, error) {
			return []plan.Op{{
				Kind: RolesName, Key: key, Name: d.Name, Action: plan.Create,
				Changes: []plan.Change{
					{Field: "organizationScopes", To: plan.List(append([]string(nil), d.OrganizationScopes...))},
					{Field: "resourceScopes", To: renderPairs(desiredPairs(env, d))},
				},
				Payload: rolePayload{d: d, create: true},
			}}, nil
		},
		Update: func(key string, d v1alpha1.OrganizationRole, have wireRole) ([]plan.Op, error) {
			return []plan.Op{roleUpdate(env, key, d, have)}, nil
		},
		Delete: func(r wireRole) plan.Op {
			return plan.Op{Kind: RolesName, Key: r.Name, Name: r.Name, Action: plan.Delete, Payload: rolePayload{id: r.ID, delete: true}}
		},
	}.Plan(observed)
}

func roleUpdate(env *engine.Env, key string, d v1alpha1.OrganizationRole, have wireRole) plan.Op {
	env.Refs.Set(RolesName, key, RefID, have.ID)
	wantScopes := append([]string(nil), d.OrganizationScopes...)
	wantPairs := desiredPairs(env, d)
	p := rolePayload{id: have.ID, d: d}
	var changes []plan.Change
	if d.Description != kindkit.Deref(have.Description) && d.Description != "" {
		changes = append(changes, plan.Change{Field: "description", From: kindkit.Deref(have.Description), To: d.Description})
		p.description = true
	}
	var haveScopes []string
	for _, s := range have.Scopes {
		haveScopes = append(haveScopes, s.Name)
	}
	if !plan.SameSet(haveScopes, wantScopes) {
		changes = append(changes, plan.Change{Field: "organizationScopes", From: plan.List(haveScopes), To: plan.List(wantScopes)})
		p.scopes = true
	}
	var havePairs []pair
	for _, s := range have.ResourceScopes {
		havePairs = append(havePairs, pair{resourceID: s.Resource.ID, scope: s.Name})
	}
	if !samePairs(havePairs, wantPairs) {
		changes = append(changes, plan.Change{Field: "resourceScopes", From: renderPairs(havePairs), To: renderPairs(wantPairs)})
		p.rscope = true
	}
	return plan.Op{Kind: RolesName, Key: key, Name: d.Name, Action: kindkit.Action(changes), Changes: changes, Payload: p}
}

func desiredPairs(env *engine.Env, d v1alpha1.OrganizationRole) []pair {
	var out []pair
	for _, rs := range d.ResourceScopes {
		out = append(out, pair{resourceID: env.Refs.Value(resources.Name, rs.Resource, resources.RefID), scope: rs.Scope})
	}
	return out
}

func samePairs(a, b []pair) bool {
	render := func(l []pair) []string {
		var out []string
		for _, p := range l {
			out = append(out, p.resourceID+"/"+p.scope)
		}
		return out
	}
	return plan.SameSet(render(a), render(b))
}

func renderPairs(l []pair) string {
	var out []string
	for _, p := range l {
		out = append(out, p.scope+"@"+p.resourceID)
	}
	sort.Strings(out)
	return plan.List(out)
}

// ids resolves the declared scopes of a role to Logto ids, known once the scopes and resources kinds have applied.
func ids(env *engine.Env, d v1alpha1.OrganizationRole) (orgScopes, resourceScopes []string, err error) {
	orgScopes, resourceScopes = []string{}, []string{}
	for _, name := range d.OrganizationScopes {
		id, ok := env.Refs.Get(ScopesName, name, RefID)
		if !ok {
			return nil, nil, fmt.Errorf("organization scope %q has no id yet", name)
		}
		orgScopes = append(orgScopes, id)
	}
	for _, rs := range d.ResourceScopes {
		id, ok := env.Refs.Get(resources.Name, rs.Resource, resources.ScopeRef(rs.Scope))
		if !ok {
			return nil, nil, fmt.Errorf("scope %q of resource %q has no id yet", rs.Scope, rs.Resource)
		}
		resourceScopes = append(resourceScopes, id)
	}
	return orgScopes, resourceScopes, nil
}

// Apply runs one op.
func (Roles) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(rolePayload)
	server := env.Client.Endpoint()
	if p.delete {
		req, err := mgmt.NewDeleteOrganizationRoleRequest(server, p.id)
		if err != nil {
			return err
		}
		return env.Client.Do(ctx, req, nil)
	}
	orgScopes, resourceScopes, err := ids(env, p.d)
	if err != nil {
		return err
	}
	if p.create {
		var created wireRole
		if err := kindkit.Send(ctx, env, newRole(p.d, orgScopes, resourceScopes), &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateOrganizationRoleRequestWithBody(s, ct, b)
		}); err != nil {
			return err
		}
		env.Refs.Set(RolesName, op.Key, RefID, created.ID)
		return nil
	}
	if p.description {
		if err := kindkit.Send(ctx, env, updateRole{Description: p.d.Description}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateOrganizationRoleRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	if p.scopes {
		if err := kindkit.Send(ctx, env, replaceScopes{OrganizationScopeIDs: orgScopes}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewReplaceOrganizationRoleScopesRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	if p.rscope {
		if err := kindkit.Send(ctx, env, replaceResourceScopes{ScopeIDs: resourceScopes}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewReplaceOrganizationRoleResourceScopesRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	return nil
}

func ptr[T any](v T) *T { return &v }
