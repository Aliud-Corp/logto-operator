package profilefields

import (
	"testing"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/logto/contract"
)

func requireValid(t *testing.T, what string, p contract.Problems, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for v, problems := range p {
		t.Errorf("%s is invalid for Logto %s: %v", what, v, problems)
	}
}

var fields = []v1alpha1.CustomProfileField{
	{
		Name: "fullname", Type: "Fullname", Label: "Full name", Required: true,
		Config: &v1alpha1.Free{Raw: []byte(`{"parts":[{"enabled":true,"name":"givenName","type":"Text","label":"Given name","required":true},` +
			`{"enabled":false,"name":"middleName","type":"Text","label":"Middle name","required":false},` +
			`{"enabled":true,"name":"familyName","type":"Text","label":"Family name","required":true}]}`)},
	},
	{Name: "company", Type: "Text", Label: "Company", Description: "Where you work"},
	{Name: "newsletter", Type: "Checkbox", Label: "Newsletter"},
}

func TestTheWireBodiesAreValidForTheWholeWindow(t *testing.T) {
	for _, d := range fields {
		p, err := contract.Request("CreateCustomProfileField", newCreate(d))
		requireValid(t, "CreateCustomProfileField "+d.Name, p, err)
		p, err = contract.Request("UpdateCustomProfileFieldByName", newUpdate(d))
		requireValid(t, "UpdateCustomProfileFieldByName "+d.Name, p, err)
	}
}

func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	p, err := contract.Response("ListCustomProfileFields", 200, []map[string]any{{
		"tenantId": "default", "id": "pf1", "name": "company", "type": "Text", "label": "Company", "description": nil,
		"required": false, "config": map[string]any{}, "createdAt": 1, "sieOrder": 1,
	}})
	requireValid(t, "ListCustomProfileFields response", p, err)
}
