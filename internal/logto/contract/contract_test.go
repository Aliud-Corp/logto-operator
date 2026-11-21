package contract_test

import (
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/contract"
)

func TestABodyValidForTheWholeWindowPasses(t *testing.T) {
	p, err := contract.Request("UpdateSignInExp", map[string]any{"unknownSessionRedirectUrl": "https://app.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(p) != 0 {
		t.Fatalf("problems = %v", p)
	}
}

// crossOriginOpenerPolicy appeared in 1.46.0: a body sending it is valid for 1.46.0 only.
func TestAFieldUnknownToOneVersionFailsForThatVersionOnly(t *testing.T) {
	p, err := contract.Request("UpdateSignInExp", map[string]any{"crossOriginOpenerPolicy": "same-origin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := p["1.46.0"]; bad {
		t.Errorf("1.46.0 refuses a field it has: %v", p["1.46.0"])
	}
	for _, v := range []string{"1.44.0", "1.45.0"} {
		if len(p[v]) == 0 {
			t.Errorf("%s accepts a field it does not have", v)
		}
	}
}
