package organizations

import "github.com/Aliud-Corp/logto-operator/api/v1alpha1"

// The kinds' own wire types: only the fields they manage. contract_test.go validates them against every vendored spec.

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

func newScope(name string, s v1alpha1.Scope) createScope {
	return createScope{Name: name, Description: s.Description}
}

type wireRole struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Scopes      []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"scopes"`
	ResourceScopes []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Resource struct {
			ID string `json:"id"`
		} `json:"resource"`
	} `json:"resourceScopes"`
}

type createRole struct {
	Name                 string   `json:"name"`
	Description          string   `json:"description,omitempty"`
	Type                 string   `json:"type"`
	OrganizationScopeIDs []string `json:"organizationScopeIds"`
	ResourceScopeIDs     []string `json:"resourceScopeIds"`
}

func newRole(d v1alpha1.OrganizationRole, orgScopes, resourceScopes []string) createRole {
	return createRole{Name: d.Name, Description: d.Description, Type: "User", OrganizationScopeIDs: orgScopes, ResourceScopeIDs: resourceScopes}
}

type updateRole struct {
	Description string `json:"description"`
}

type replaceScopes struct {
	OrganizationScopeIDs []string `json:"organizationScopeIds"`
}

type replaceResourceScopes struct {
	ScopeIDs []string `json:"scopeIds"`
}
