package roles

import "github.com/Aliud-Corp/logto-operator/api/v1alpha1"

// The kind's own wire types: only the fields it manages. contract_test.go validates them against every vendored spec.

type wireRole struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	IsDefault   bool   `json:"isDefault"`
}

type wireScope struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Resource struct {
		Indicator string `json:"indicator"`
	} `json:"resource"`
}

type createRole struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	IsDefault   *bool    `json:"isDefault,omitempty"`
	ScopeIDs    []string `json:"scopeIds,omitempty"`
}

func newCreate(d v1alpha1.Role, scopeIDs []string) createRole {
	return createRole{Name: d.Name, Description: d.Description, Type: d.Type, IsDefault: d.IsDefault, ScopeIDs: scopeIDs}
}

type updateRole struct {
	Description *string `json:"description,omitempty"`
	IsDefault   *bool   `json:"isDefault,omitempty"`
}

type addScopes struct {
	ScopeIDs []string `json:"scopeIds"`
}
