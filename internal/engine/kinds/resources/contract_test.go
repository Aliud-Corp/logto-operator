package resources

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

// Every body this kind can send, valid for every Logto of the support window.
func TestTheWireBodiesAreValidForTheWholeWindow(t *testing.T) {
	full := v1alpha1.Resource{Name: "Example App API", Indicator: "https://app.example.test/api", AccessTokenTTL: 3600}
	bare := v1alpha1.Resource{Name: "Example", Indicator: "https://x.example.test/api"}
	for _, d := range []v1alpha1.Resource{full, bare} {
		p, err := contract.Request("CreateResource", newCreate(d))
		requireValid(t, "CreateResource", p, err)
		p, err = contract.Request("UpdateResource", newUpdate(d, wireResource{}))
		requireValid(t, "UpdateResource", p, err)
	}
	p, err := contract.Request("CreateResourceScope", newScope("event:read", v1alpha1.Scope{Description: "Read"}))
	requireValid(t, "CreateResourceScope", p, err)
	p, err = contract.Request("CreateResourceScope", newScope("event:read", v1alpha1.Scope{}))
	requireValid(t, "CreateResourceScope without description", p, err)
	desc := "Read"
	p, err = contract.Request("UpdateResourceScope", updateScope{Description: &desc})
	requireValid(t, "UpdateResourceScope", p, err)
}

// What the kind decodes exists in every version's responses.
func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	desc := "Read"
	p, err := contract.Response("ListResources", 200, []map[string]any{{
		"tenantId": "default", "id": "r1", "name": "n", "indicator": "https://a.test/api", "isDefault": false, "accessTokenTtl": 3600,
	}})
	requireValid(t, "ListResources response", p, err)
	p, err = contract.Response("ListResourceScopes", 200, []map[string]any{{
		"tenantId": "default", "id": "s1", "resourceId": "r1", "name": "event:read", "description": desc, "createdAt": 1,
	}})
	requireValid(t, "ListResourceScopes response", p, err)
}
