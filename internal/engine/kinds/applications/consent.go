package applications

import (
	"bytes"
	"context"
	"net/http"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/consentscopes"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// consentCreate is the consent of an application this run creates: everything declared is added.
func consentCreate(env *engine.Env, d v1alpha1.Application) (consentscopes.Change, []plan.Change) {
	return consentscopes.Create(env, d.ConsentScopes)
}

// consentUpdate compares the declared consent scopes with what the application holds: a declared list is exact.
func consentUpdate(ctx context.Context, env *engine.Env, d v1alpha1.Application, id string) (consentscopes.Change, []plan.Change, error) {
	if !consentscopes.Declared(d.ConsentScopes) {
		return consentscopes.Change{}, nil, nil
	}
	req, err := mgmt.NewListApplicationUserConsentScopesRequest(env.Client.Endpoint(), id)
	if err != nil {
		return consentscopes.Change{}, nil, err
	}
	var held consentscopes.Held
	if err := env.Client.Do(ctx, req, &held); err != nil {
		return consentscopes.Change{}, nil, err
	}
	c, changes := consentscopes.Diff(env, d.ConsentScopes, held)
	return c, changes, nil
}

// applyConsent adds, then removes, an application's consent scopes.
func applyConsent(ctx context.Context, env *engine.Env, id string, c consentscopes.Change) error {
	return consentscopes.Apply(env, c,
		func(body consentscopes.Add) error {
			return kindkit.Send(ctx, env, body, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
				return mgmt.NewCreateApplicationUserConsentScopeRequestWithBody(s, id, ct, b)
			})
		},
		func(scopeType, scopeID string) error {
			req, err := mgmt.NewDeleteApplicationUserConsentScopeRequest(env.Client.Endpoint(), id, mgmt.DeleteApplicationUserConsentScopeParamsScopeType(scopeType), scopeID)
			if err != nil {
				return err
			}
			return env.Client.Do(ctx, req, nil)
		})
}
