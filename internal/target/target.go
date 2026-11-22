// Package target connects to the Logto a document names: its credentials resolved, its own spec read for the
// capability check. logtoctl and the operator reach Logto through it alike.
package target

import (
	"context"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
)

// Connect builds the environment of one run against the document's Logto.
func Connect(ctx context.Context, s *v1alpha1.LogtoConfigSpec, resolver secrets.Resolver) (*engine.Env, error) {
	id, err := resolver.String("spec.credentials.clientId", s.Credentials.ClientID)
	if err != nil {
		return nil, err
	}
	secret, err := resolver.Secret("spec.credentials.clientSecret", s.Credentials.ClientSecret)
	if err != nil {
		return nil, err
	}
	c := client.New(client.Config{Endpoint: s.Endpoint, TokenEndpoint: s.Credentials.TokenEndpoint, ClientID: id, ClientSecret: secret.Reveal()})
	spec, err := capability.Fetch(ctx, c)
	if err != nil {
		return nil, err
	}
	return &engine.Env{Client: c, Spec: s, Secrets: resolver, Capability: spec}, nil
}
