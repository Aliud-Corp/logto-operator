// Package cimd reconciles the tenant's Client ID Metadata Document setting, a singleton: whether CIMD is on, its
// experimental offline-access switch, and the consent ceiling (tenant-wide scopes) of its clients. An absent
// spec.cimd is not managed at all: nothing is read, nothing is written.
package cimd

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/consentscopes"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Name of the kind.
const Name = "cimd"

// Kind reconciles spec.cimd.
type Kind struct{}

func (Kind) Name() string { return Name }

// DependsOn: a consent scope names a resource scope, whose id the resources kind records.
func (Kind) DependsOn() []string { return []string{resources.Name} }

// Needs lists what the kind can send.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	d := env.Spec.CIMD
	if d == nil {
		return nil, nil
	}
	body := wireConfig{Enabled: d.Enabled, AddConsentPromptForOfflineAccess: d.AddConsentPromptForOfflineAccess}
	update, err := kindkit.Need("UpdateCimdConfig", "spec.cimd", body)
	if err != nil {
		return nil, err
	}
	needs := []capability.Need{{OperationID: "GetCimdConfig", Where: "spec.cimd"}, update}
	if cs := d.ConsentScopes; cs != nil {
		add := consentscopes.Add{}
		if cs.Resources != nil {
			add.ResourceScopes = []string{"id"}
		}
		if cs.User != nil {
			add.UserScopes = []string{"profile"}
		}
		need, err := kindkit.Need("AssignCimdUserConsentScopes", "spec.cimd.consentScopes", add)
		if err != nil {
			return nil, err
		}
		needs = append(needs, need,
			capability.Need{OperationID: "ListCimdUserConsentScopes", Where: "spec.cimd.consentScopes"},
			capability.Need{OperationID: "DeleteCimdUserConsentScope", Where: "spec.cimd.consentScopes"})
	}
	return needs, nil
}

// payload is what an apply sends: the changed keys of the config, then the consent scopes.
type payload struct {
	patch   map[string]any
	consent consentscopes.Change
}

// Plan observes the config and the consent ceiling and diffs what the document declares.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	d := env.Spec.CIMD
	if d == nil {
		return nil, nil
	}
	req, err := mgmt.NewGetCimdConfigRequest(env.Client.Endpoint())
	if err != nil {
		return nil, err
	}
	var have wireConfig
	if err := env.Client.Do(ctx, req, &have); err != nil {
		return nil, err
	}
	p := payload{patch: map[string]any{}}
	var changes []plan.Change
	if d.Enabled != have.Enabled {
		changes = append(changes, plan.Change{Field: "enabled", From: strconv.FormatBool(have.Enabled), To: strconv.FormatBool(d.Enabled)})
		p.patch["enabled"] = d.Enabled
	}
	if w := d.AddConsentPromptForOfflineAccess; w != nil {
		held := have.AddConsentPromptForOfflineAccess != nil && *have.AddConsentPromptForOfflineAccess
		if *w != held {
			changes = append(changes, plan.Change{Field: "addConsentPromptForOfflineAccess", From: strconv.FormatBool(held), To: strconv.FormatBool(*w)})
			p.patch["addConsentPromptForOfflineAccess"] = *w
		}
	}
	if consentscopes.Declared(d.ConsentScopes) {
		list, err := mgmt.NewListCimdUserConsentScopesRequest(env.Client.Endpoint())
		if err != nil {
			return nil, err
		}
		var held consentscopes.Held
		if err := env.Client.Do(ctx, list, &held); err != nil {
			return nil, err
		}
		c, cc := consentscopes.Diff(env, d.ConsentScopes, held)
		p.consent = c
		changes = append(changes, cc...)
	}
	return []plan.Op{{Kind: Name, Key: "default", Action: kindkit.Action(changes), Changes: changes, Payload: p}}, nil
}

// Apply sends the PATCH of the changed keys, then the consent scopes. Logto refuses to change the config (422)
// while its outbound SSRF protection is disabled or restricted by an allowlist: the refusal is reported with that
// cause, not worked around.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(payload)
	if len(p.patch) > 0 {
		err := kindkit.Send(ctx, env, p.patch, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateCimdConfigRequestWithBody(s, ct, b)
		})
		if client.IsStatus(err, http.StatusUnprocessableEntity) {
			return fmt.Errorf("spec.cimd: Logto refused the CIMD configuration (%w); CIMD needs Logto's outbound SSRF "+
				"protection on, with no allowlist (check SSRF_PROTECTION_DISABLED, OIDC_PROVIDER_SSRF_PROTECTION_DISABLED "+
				"and SSRF_ALLOWED_ADDRESSES on the Logto instance)", err)
		}
		if err != nil {
			return err
		}
	}
	return consentscopes.Apply(env, p.consent,
		func(body consentscopes.Add) error {
			return kindkit.Send(ctx, env, body, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
				return mgmt.NewAssignCimdUserConsentScopesRequestWithBody(s, ct, b)
			})
		},
		func(scopeType, scopeID string) error {
			req, err := mgmt.NewDeleteCimdUserConsentScopeRequest(env.Client.Endpoint(), mgmt.DeleteCimdUserConsentScopeParamsScopeType(scopeType), scopeID)
			if err != nil {
				return err
			}
			return env.Client.Do(ctx, req, nil)
		})
}
