// Package resources reconciles API resources (matched by indicator) and their scopes (by name). A scope Logto holds
// and the document does not declare is never removed silently: removing a scope revokes it from every token, so the
// plan refuses unless spec.prune.resources is set.
package resources

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Name of the kind.
const Name = "resources"

// Kind reconciles spec.resources.
type Kind struct{}

func (Kind) Name() string        { return Name }
func (Kind) DependsOn() []string { return nil }

// Refs this kind records: "id", "indicator", and "scope:<name>" (the scope's id).
const RefID, RefIndicator = "id", "indicator"

// ScopeRef is the ref field of a scope's id.
func ScopeRef(name string) string { return "scope:" + name }

// protected resources are never pruned: Logto's own Management API.
func protected(r wireResource) bool {
	return strings.HasPrefix(r.Indicator, "https://default.logto.app")
}

// payload is what Apply needs for one op.
type payload struct {
	id           string // existing resource id
	create       *createResource
	update       *updateResource
	scopeCreates []createScope
	scopeUpdates map[string]updateScope // by scope id
	scopeDeletes []string               // scope ids
	delete       bool
	key          string
}

// Needs lists every body this kind can send.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, key := range kindkit.Keys(env.Spec.Resources) {
		r := env.Spec.Resources[key]
		where := "spec.resources." + key
		for _, n := range []struct {
			op   string
			body any
		}{{"CreateResource", newCreate(r)}, {"UpdateResource", newUpdate(r, wireResource{})}} {
			need, err := kindkit.Need(n.op, where, n.body)
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
		for _, name := range kindkit.Keys(r.Scopes) {
			need, err := kindkit.Need("CreateResourceScope", where+".scopes."+name, newScope(name, r.Scopes[name]))
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
	}
	return needs, nil
}

func listResources(ctx context.Context, env *engine.Env) ([]wireResource, error) {
	return kindkit.List[wireResource](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListResourcesRequest(env.Client.Endpoint(), &mgmt.ListResourcesParams{Page: p.Page, PageSize: p.Size})
	})
}

func listScopes(ctx context.Context, env *engine.Env, resourceID string) ([]wireScope, error) {
	return kindkit.List[wireScope](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListResourceScopesRequest(env.Client.Endpoint(), resourceID, &mgmt.ListResourceScopesParams{Page: p.Page, PageSize: p.Size})
	})
}

// Plan observes the resources and their scopes and diffs them with the document.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	observed, err := listResources(ctx, env)
	if err != nil {
		return nil, err
	}
	byIndicator := map[string]wireResource{}
	for _, r := range observed {
		byIndicator[r.Indicator] = r
	}
	declared := map[string]bool{}
	var ops []plan.Op
	for _, key := range kindkit.Keys(env.Spec.Resources) {
		d := env.Spec.Resources[key]
		declared[d.Indicator] = true
		env.Refs.Set(Name, key, RefIndicator, d.Indicator)
		have, exists := byIndicator[d.Indicator]
		if !exists {
			ops = append(ops, createOp(key, d))
			continue
		}
		op, err := updateOp(ctx, env, key, d, have)
		if err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	if env.Spec.Prune.Resources {
		for _, r := range observed {
			if declared[r.Indicator] || protected(r) || kindkit.Kept(env, r.Name) {
				continue
			}
			ops = append(ops, plan.Op{
				Kind: Name, Key: r.Indicator, Name: r.Name, Action: plan.Delete,
				Changes: []plan.Change{{Field: "indicator", From: r.Indicator}},
				Payload: payload{id: r.ID, delete: true},
			})
		}
	}
	return ops, nil
}

func createOp(key string, d v1alpha1.Resource) plan.Op {
	changes := []plan.Change{{Field: "name", To: d.Name}, {Field: "indicator", To: d.Indicator}}
	if d.AccessTokenTTL != 0 {
		changes = append(changes, plan.Change{Field: "accessTokenTtl", To: strconv.Itoa(int(d.AccessTokenTTL))})
	}
	p := payload{key: key, create: newCreate(d)}
	for _, name := range kindkit.Keys(d.Scopes) {
		changes = append(changes, plan.Change{Field: "scopes." + name, To: "added"})
		p.scopeCreates = append(p.scopeCreates, newScope(name, d.Scopes[name]))
	}
	return plan.Op{Kind: Name, Key: key, Name: d.Name, Action: plan.Create, Changes: changes, Payload: p}
}

func updateOp(ctx context.Context, env *engine.Env, key string, d v1alpha1.Resource, have wireResource) (plan.Op, error) {
	env.Refs.Set(Name, key, RefID, have.ID)
	scopes, err := listScopes(ctx, env, have.ID)
	if err != nil {
		return plan.Op{}, err
	}
	p := payload{key: key, id: have.ID, scopeUpdates: map[string]updateScope{}}
	var changes []plan.Change
	changes = kindkit.Field(changes, "name", have.Name, d.Name)
	if d.AccessTokenTTL != 0 {
		changes = kindkit.Field(changes, "accessTokenTtl", strconv.Itoa(have.AccessTokenTTL), strconv.Itoa(int(d.AccessTokenTTL)))
	}
	if len(changes) > 0 {
		p.update = newUpdate(d, have)
	}
	byName := map[string]wireScope{}
	for _, s := range scopes {
		byName[s.Name] = s
		env.Refs.Set(Name, key, ScopeRef(s.Name), s.ID)
	}
	for _, name := range kindkit.Keys(d.Scopes) {
		want := d.Scopes[name]
		s, exists := byName[name]
		switch {
		case !exists:
			changes = append(changes, plan.Change{Field: "scopes." + name, To: "added"})
			p.scopeCreates = append(p.scopeCreates, newScope(name, want))
		case want.Description != "" && deref(s.Description) != want.Description:
			changes = append(changes, plan.Change{Field: "scopes." + name + ".description", From: deref(s.Description), To: want.Description})
			desc := want.Description
			p.scopeUpdates[s.ID] = updateScope{Description: &desc}
		}
	}
	for _, s := range scopes {
		if _, ok := d.Scopes[s.Name]; ok {
			continue
		}
		if !env.Spec.Prune.Resources {
			return plan.Op{}, fmt.Errorf("spec.resources.%s: Logto holds the scope %q that the document does not declare; "+
				"removing a scope revokes it from every token: declare it, or set spec.prune.resources to remove it", key, s.Name)
		}
		changes = append(changes, plan.Change{Field: "scopes." + s.Name, From: "present", To: "removed"})
		p.scopeDeletes = append(p.scopeDeletes, s.ID)
	}
	return plan.Op{Kind: Name, Key: key, Name: d.Name, Action: kindkit.Action(changes), Changes: changes, Payload: p}, nil
}

// Apply runs one op.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(payload)
	server := env.Client.Endpoint()
	if p.delete {
		req, err := mgmt.NewDeleteResourceRequest(server, p.id)
		if err != nil {
			return err
		}
		return env.Client.Do(ctx, req, nil)
	}
	if p.create != nil {
		var created wireResource
		if err := kindkit.Send(ctx, env, p.create, &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateResourceRequestWithBody(s, ct, b)
		}); err != nil {
			return err
		}
		p.id = created.ID
		env.Refs.Set(Name, p.key, RefID, created.ID)
	}
	if p.update != nil {
		if err := kindkit.Send(ctx, env, p.update, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateResourceRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	for _, sc := range p.scopeCreates {
		var created wireScope
		if err := kindkit.Send(ctx, env, sc, &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateResourceScopeRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return fmt.Errorf("scope %s: %w", sc.Name, err)
		}
		env.Refs.Set(Name, p.key, ScopeRef(created.Name), created.ID)
	}
	for _, id := range kindkit.Keys(p.scopeUpdates) {
		if err := kindkit.Send(ctx, env, p.scopeUpdates[id], nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateResourceScopeRequestWithBody(s, p.id, id, ct, b)
		}); err != nil {
			return err
		}
	}
	for _, id := range p.scopeDeletes {
		req, err := mgmt.NewDeleteResourceScopeRequest(server, p.id, id)
		if err != nil {
			return err
		}
		if err := env.Client.Do(ctx, req, nil); err != nil {
			return err
		}
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
