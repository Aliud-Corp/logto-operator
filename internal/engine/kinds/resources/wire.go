package resources

import "github.com/Aliud-Corp/logto-operator/api/v1alpha1"

// The kind's own wire types: only the fields it manages. contract_test.go validates them against every vendored spec.

type wireResource struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Indicator      string `json:"indicator"`
	AccessTokenTTL int    `json:"accessTokenTtl"`
}

type createResource struct {
	Name           string `json:"name"`
	Indicator      string `json:"indicator"`
	AccessTokenTTL *int   `json:"accessTokenTtl,omitempty"`
}

type updateResource struct {
	Name           string `json:"name"`
	AccessTokenTTL *int   `json:"accessTokenTtl,omitempty"`
}

type wireScope struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type createScope struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type updateScope struct {
	Description *string `json:"description,omitempty"`
}

func ttl(d v1alpha1.Resource) *int {
	if d.AccessTokenTTL == 0 {
		return nil
	}
	v := int(d.AccessTokenTTL)
	return &v
}

func newCreate(d v1alpha1.Resource) *createResource {
	return &createResource{Name: d.Name, Indicator: d.Indicator, AccessTokenTTL: ttl(d)}
}

func newUpdate(d v1alpha1.Resource, _ wireResource) *updateResource {
	return &updateResource{Name: d.Name, AccessTokenTTL: ttl(d)}
}

func newScope(name string, s v1alpha1.Scope) createScope {
	return createScope{Name: name, Description: s.Description}
}
