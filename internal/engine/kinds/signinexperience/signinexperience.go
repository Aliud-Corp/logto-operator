// Package signinexperience reconciles the declared keys of the sign-in experience, a singleton: every key the
// document does not declare is left as Logto has it. Logto's PATCH needs some sub-objects whole (color,
// languageInfo, signUp, each sign-in method): an undeclared part of such a key is taken from what Logto holds.
package signinexperience

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/connectors"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Name of the kind.
const Name = "signInExperience"

// Kind reconciles spec.signInExperience.
type Kind struct{}

func (Kind) Name() string        { return Name }
func (Kind) DependsOn() []string { return []string{connectors.Name} }

// Needs lists the fields the PATCH can send.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	d := env.Spec.SignInExperience
	if d == nil {
		return nil, nil
	}
	need, err := kindkit.Need("UpdateSignInExp", "spec.signInExperience", desired(d, wireSIE{}))
	if err != nil {
		return nil, err
	}
	return []capability.Need{need}, nil
}

// Plan observes the sign-in experience and diffs the declared keys.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	d := env.Spec.SignInExperience
	if d == nil {
		return nil, nil
	}
	if err := checkEmailConnector(ctx, env, d); err != nil {
		return nil, err
	}
	req, err := mgmt.NewGetSignInExpRequest(env.Client.Endpoint())
	if err != nil {
		return nil, err
	}
	var have wireSIE
	if err := env.Client.Do(ctx, req, &have); err != nil {
		return nil, err
	}
	want := desired(d, have)
	patch := patchSIE{}
	var changes []plan.Change
	cmp := func(field string, w, h any, set func()) {
		if w == nil || kindkit.Subset(w, h) && kindkit.Subset(h, w) {
			return
		}
		changes = append(changes, plan.Change{Field: field, From: kindkit.JSON(h), To: kindkit.JSON(w)})
		set()
	}
	if want.Color != nil {
		cmp("color", want.Color, have.Color, func() { patch.Color = want.Color })
	}
	if want.LanguageInfo != nil {
		cmp("languageInfo", want.LanguageInfo, have.LanguageInfo, func() { patch.LanguageInfo = want.LanguageInfo })
	}
	if want.Branding != nil {
		cmp("branding", want.Branding, have.Branding, func() { patch.Branding = want.Branding })
	}
	if want.CustomCSS != nil {
		cmp("customCss", deref(want.CustomCSS), deref(have.CustomCSS), func() { patch.CustomCSS = want.CustomCSS })
	}
	if want.SignUp != nil {
		cmp("signUp", want.SignUp, have.SignUp, func() { patch.SignUp = want.SignUp })
	}
	if want.SignIn != nil {
		cmp("signIn", want.SignIn, have.SignIn, func() { patch.SignIn = want.SignIn })
	}
	if want.UnknownSessionRedirectURL != nil {
		cmp("unknownSessionRedirectUrl", deref(want.UnknownSessionRedirectURL), deref(have.UnknownSessionRedirectURL),
			func() { patch.UnknownSessionRedirectURL = want.UnknownSessionRedirectURL })
	}
	return []plan.Op{{Kind: Name, Key: "default", Action: kindkit.Action(changes), Changes: changes, Payload: patch}}, nil
}

// checkEmailConnector refuses an e-mail sign-up identifier with verification when no e-mail connector is declared
// or present: Logto refuses that PATCH, three calls later and with a vaguer message.
func checkEmailConnector(ctx context.Context, env *engine.Env, d *v1alpha1.SignInExperience) error {
	if d.SignUp == nil || !d.SignUp.Verify || !contains(d.SignUp.Identifiers, "email") {
		return nil
	}
	for _, k := range kindkit.Keys(env.Spec.Connectors) {
		if _, ok := env.Refs.Get(connectors.Name, k, connectors.RefEmail); ok {
			return nil
		}
	}
	req, err := mgmt.NewListConnectorsRequest(env.Client.Endpoint(), &mgmt.ListConnectorsParams{})
	if err != nil {
		return err
	}
	var list []struct {
		Type string `json:"type"`
	}
	if err := env.Client.Do(ctx, req, &list); err != nil {
		return err
	}
	for _, c := range list {
		if c.Type == "Email" {
			return nil
		}
	}
	return errors.New("spec.signInExperience.signUp: an e-mail identifier with verify needs an e-mail connector; " +
		"declare one in spec.connectors")
}

// Apply sends the PATCH of the changed keys.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	return kindkit.Send(ctx, env, op.Payload, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
		return mgmt.NewUpdateSignInExpRequestWithBody(s, &mgmt.UpdateSignInExpParams{}, ct, b)
	})
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
