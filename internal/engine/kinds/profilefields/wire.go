package profilefields

import (
	"encoding/json"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
)

// The kind's own wire types: only the fields it manages. contract_test.go validates them against every vendored spec.

type wireField struct {
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Label       string  `json:"label"`
	Description *string `json:"description"`
	Required    bool    `json:"required"`
	Config      any     `json:"config"`
}

// updateField is the body of PUT /api/custom-profile-fields/{name}: required is mandatory, even when false.
type updateField struct {
	Type        string          `json:"type"`
	Label       string          `json:"label,omitempty"`
	Description string          `json:"description,omitempty"`
	Required    bool            `json:"required"`
	Config      json.RawMessage `json:"config,omitempty"`
}

type createField struct {
	Name string `json:"name"`
	updateField
}

func newUpdate(d v1alpha1.CustomProfileField) updateField {
	u := updateField{Type: d.Type, Label: d.Label, Description: d.Description, Required: d.Required}
	if d.Config != nil && len(d.Config.Raw) > 0 {
		u.Config = d.Config.Raw
	}
	return u
}

func newCreate(d v1alpha1.CustomProfileField) createField {
	return createField{Name: d.Name, updateField: newUpdate(d)}
}
