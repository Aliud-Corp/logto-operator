// Package roles reconciles global roles (the tenant's role-based access control), matched by name. A role's type
// cannot change in Logto; a role whose scopes are declared holds exactly those. Prune never deletes a role the
// document grants, nor a role holding a scope of Logto's Management API, and declared scopes never take such a
// scope away: either would cut the access of every application the role is granted to, this tool's own included.
package roles

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Name of the kind.
const Name = "roles"

// RefID is the ref field of a role's id.
const RefID = "id"

// Kind reconciles spec.roles.
type Kind struct{}

func (Kind) Name() string        { return Name }
func (Kind) DependsOn() []string { return []string{resources.Name} }

// Declared returns the document key of the declared role with this Logto name, for the kinds that grant roles.
func Declared(spec *v1alpha1.LogtoConfigSpec, name string) (string, v1alpha1.Role, bool) {
	for _, key := range kindkit.Keys(spec.Roles) {
		if r := spec.Roles[key]; r.Name == name {
			return key, r, true
		}
	}
	return "", v1alpha1.Role{}, false
}

// Needs lists every body this kind can send.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, key := range kindkit.Keys(env.Spec.Roles) {
		d := env.Spec.Roles[key]
		where := "spec.roles." + key
		desc := d.Description
		bodies := map[string]any{"CreateRole": newCreate(d, []string{"id"}), "UpdateRole": updateRole{Description: &desc, IsDefault: d.IsDefault}}
		if d.Scopes != nil {
			bodies["CreateRoleScope"] = addScopes{ScopeIDs: []string{}}
			needs = append(needs, capability.Need{OperationID: "DeleteRoleScope", Where: where + ".scopes"})
		}
		for _, op := range kindkit.Keys(bodies) {
			need, err := kindkit.Need(op, where, bodies[op])
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
	}
	return append(needs, kindkit.Deletes(env.Spec.Prune.Roles, "spec.prune.roles", "DeleteRole")...), nil
}

type payload struct {
	id     string
	key    string
	d      v1alpha1.Role
	create bool
	delete bool
	update *updateRole
	add    []v1alpha1.ResourceScopeRef // scopes to add, resolved to ids at apply
	remove []string                    // scope ids to remove
}

func listScopes(ctx context.Context, env *engine.Env, id string) ([]wireScope, error) {
	return kindkit.List[wireScope](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListRoleScopesRequest(env.Client.Endpoint(), id, &mgmt.ListRoleScopesParams{Page: p.Page, PageSize: p.Size})
	})
}

// scopeKey is how a scope is compared: its resource's indicator and its name.
func scopeKey(indicator, name string) string { return indicator + " " + name }

// Plan observes the roles.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	if len(env.Spec.Roles) == 0 && !env.Spec.Prune.Roles {
		return nil, nil
	}
	observed, err := kindkit.List[wireRole](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListRolesRequest(env.Client.Endpoint(), &mgmt.ListRolesParams{Page: p.Page, PageSize: p.Size})
	})
	if err != nil {
		return nil, err
	}
	protected := map[string]bool{}
	if env.Spec.Prune.Roles {
		granted := grantedNames(env.Spec)
		for _, r := range observed {
			if _, _, ok := Declared(env.Spec, r.Name); ok || kindkit.Kept(env, r.Name) {
				continue
			}
			if granted[r.Name] {
				protected[r.ID] = true
				continue
			}
			scopes, err := listScopes(ctx, env, r.ID)
			if err != nil {
				return nil, err
			}
			for _, s := range scopes {
				protected[r.ID] = protected[r.ID] || kindkit.ManagementAPI(s.Resource.Indicator)
			}
		}
	}
	return kindkit.Collection[v1alpha1.Role, wireRole]{
		Kind: Name, Where: "spec.roles", Declared: env.Spec.Roles,
		Natural:   func(_ string, d v1alpha1.Role) string { return d.Name },
		Held:      func(r wireRole) string { return r.Name },
		Prune:     env.Spec.Prune.Roles,
		Keep:      env.Spec.Prune.Keep,
		Protected: func(r wireRole) bool { return protected[r.ID] },
		Create: func(key string, d v1alpha1.Role) ([]plan.Op, error) {
			changes := []plan.Change{{Field: "type", To: d.Type}, {Field: "description", To: d.Description}}
			if d.IsDefault != nil {
				changes = append(changes, plan.Change{Field: "isDefault", To: strconv.FormatBool(*d.IsDefault)})
			}
			if d.Scopes != nil {
				changes = append(changes, plan.Change{Field: "scopes", To: plan.List(wantedScopes(env, *d.Scopes))})
			}
			return []plan.Op{{Kind: Name, Key: key, Name: d.Name, Action: plan.Create, Changes: changes, Payload: payload{key: key, d: d, create: true}}}, nil
		},
		Update: func(key string, d v1alpha1.Role, have wireRole) ([]plan.Op, error) {
			op, err := updateOp(ctx, env, key, d, have)
			return []plan.Op{op}, err
		},
		Delete: func(r wireRole) plan.Op {
			return plan.Op{Kind: Name, Key: r.Name, Name: r.Name, Action: plan.Delete, Changes: []plan.Change{{Field: "type", From: r.Type}}, Payload: payload{id: r.ID, delete: true}}
		},
	}.Plan(observed)
}

// grantedNames are the roles the document grants to applications and users, by Logto name.
func grantedNames(spec *v1alpha1.LogtoConfigSpec) map[string]bool {
	out := map[string]bool{}
	for _, a := range spec.Applications {
		for _, r := range a.Roles {
			out[r.Name] = true
		}
	}
	for _, u := range spec.Users {
		for _, r := range u.Roles {
			out[r.Name] = true
		}
	}
	return out
}

func wantedScopes(env *engine.Env, refs []v1alpha1.ResourceScopeRef) []string {
	out := []string{}
	for _, ref := range refs {
		out = append(out, scopeKey(env.Spec.Resources[ref.Resource].Indicator, ref.Scope))
	}
	sort.Strings(out)
	return out
}

func updateOp(ctx context.Context, env *engine.Env, key string, d v1alpha1.Role, have wireRole) (plan.Op, error) {
	path := "spec.roles." + key
	if have.Type != d.Type {
		return plan.Op{}, fmt.Errorf("%s: the role exists with type %s; Logto cannot change a role's type (declared %s)", path, have.Type, d.Type)
	}
	env.Refs.Set(Name, key, RefID, have.ID)
	p := payload{id: have.ID, key: key, d: d}
	var changes []plan.Change
	var update updateRole
	if have.Description != d.Description {
		changes = append(changes, plan.Change{Field: "description", From: have.Description, To: d.Description})
		desc := d.Description
		update.Description = &desc
	}
	if d.IsDefault != nil && *d.IsDefault != have.IsDefault {
		changes = append(changes, plan.Change{Field: "isDefault", From: strconv.FormatBool(have.IsDefault), To: strconv.FormatBool(*d.IsDefault)})
		update.IsDefault = d.IsDefault
	}
	if update != (updateRole{}) {
		p.update = &update
	}
	if d.Scopes != nil {
		held, err := listScopes(ctx, env, have.ID)
		if err != nil {
			return plan.Op{}, err
		}
		heldBy := map[string]wireScope{}
		var from []string
		for _, s := range held {
			k := scopeKey(s.Resource.Indicator, s.Name)
			heldBy[k] = s
			from = append(from, k)
		}
		want := map[string]bool{}
		for _, ref := range *d.Scopes {
			k := scopeKey(env.Spec.Resources[ref.Resource].Indicator, ref.Scope)
			want[k] = true
			if _, ok := heldBy[k]; !ok {
				p.add = append(p.add, ref)
			}
		}
		for _, s := range held {
			if want[scopeKey(s.Resource.Indicator, s.Name)] {
				continue
			}
			if kindkit.ManagementAPI(s.Resource.Indicator) {
				return plan.Op{}, fmt.Errorf("%s: removing the scope %q of Logto's Management API (%s) would cut the access of every "+
					"application holding the role; declare it among the scopes, or leave scopes out to keep what Logto holds", path, s.Name, s.Resource.Indicator)
			}
			p.remove = append(p.remove, s.ID)
		}
		if len(p.add) > 0 || len(p.remove) > 0 {
			sort.Strings(from)
			changes = append(changes, plan.Change{Field: "scopes", From: plan.List(from), To: plan.List(wantedScopes(env, *d.Scopes))})
		}
	}
	return plan.Op{Kind: Name, Key: key, Name: d.Name, Action: kindkit.Action(changes), Changes: changes, Payload: p}, nil
}

// scopeIDs resolves declared scopes to Logto ids, known once the resources kind has applied.
func scopeIDs(env *engine.Env, refs []v1alpha1.ResourceScopeRef) ([]string, error) {
	out := []string{}
	for _, ref := range refs {
		id, ok := env.Refs.Get(resources.Name, ref.Resource, resources.ScopeRef(ref.Scope))
		if !ok {
			return nil, fmt.Errorf("scope %q of resource %q has no id yet", ref.Scope, ref.Resource)
		}
		out = append(out, id)
	}
	return out, nil
}

// Apply runs one op.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(payload)
	server := env.Client.Endpoint()
	if p.delete {
		req, err := mgmt.NewDeleteRoleRequest(server, p.id)
		if err != nil {
			return err
		}
		return env.Client.Do(ctx, req, nil)
	}
	if p.create {
		var ids []string
		if p.d.Scopes != nil {
			var err error
			if ids, err = scopeIDs(env, *p.d.Scopes); err != nil {
				return err
			}
		}
		var created wireRole
		if err := kindkit.Send(ctx, env, newCreate(p.d, ids), &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateRoleRequestWithBody(s, ct, b)
		}); err != nil {
			return err
		}
		env.Refs.Set(Name, p.key, RefID, created.ID)
		return nil
	}
	if p.update != nil {
		if err := kindkit.Send(ctx, env, p.update, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateRoleRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	if len(p.add) > 0 {
		ids, err := scopeIDs(env, p.add)
		if err != nil {
			return err
		}
		if err := kindkit.Send(ctx, env, addScopes{ScopeIDs: ids}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateRoleScopeRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	for _, id := range p.remove {
		req, err := mgmt.NewDeleteRoleScopeRequest(server, p.id, id)
		if err != nil {
			return err
		}
		if err := env.Client.Do(ctx, req, nil); err != nil {
			return err
		}
	}
	return nil
}
