package signinexperience

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

func TestTheWireBodiesAreValidForTheWholeWindow(t *testing.T) {
	full := &v1alpha1.SignInExperience{
		Color:        &v1alpha1.Color{PrimaryColor: "#101418"},
		LanguageInfo: &v1alpha1.LanguageInfo{FallbackLanguage: "fr"},
		Branding:     &v1alpha1.Branding{LogoURL: &v1alpha1.TextSource{Value: "data:image/svg+xml;base64,PHN2Zy8+"}},
		CustomCSS:    &v1alpha1.TextSource{Value: ".x{}"},
		SignUp:       &v1alpha1.SignUp{Identifiers: []string{"email"}, Password: true, Verify: true},
		SignIn: &v1alpha1.SignIn{Methods: []v1alpha1.SignInMethod{
			{Identifier: "email", Password: true, VerificationCode: true, IsPasswordPrimary: true},
		}},
		UnknownSessionRedirectURL: "https://app.example.test",
	}
	have := wireSIE{Color: &color{DarkPrimaryColor: "#8C6CFF"}}
	p, err := contract.Request("UpdateSignInExp", desired(full, have))
	requireValid(t, "UpdateSignInExp (every managed key)", p, err)
	p, err = contract.Request("UpdateSignInExp", desired(&v1alpha1.SignInExperience{Color: &v1alpha1.Color{PrimaryColor: "#101418"}}, wireSIE{}))
	requireValid(t, "UpdateSignInExp (colour only, nothing observed)", p, err)
}

func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	p, err := contract.ResponseFields("GetSignInExp", 200, map[string]any{
		"color":        map[string]any{"primaryColor": "#6139F6", "isDarkModeEnabled": false, "darkPrimaryColor": "#8C6CFF"},
		"languageInfo": map[string]any{"autoDetect": true, "fallbackLanguage": "en"},
		"branding":     map[string]any{"logoUrl": "https://x.test/l.svg", "darkLogoUrl": "https://x.test/d.svg", "favicon": "https://x.test/f.ico"},
		"customCss":    nil,
		"signUp":       map[string]any{"identifiers": []any{"username"}, "password": true, "verify": false},
		"signIn": map[string]any{"methods": []any{map[string]any{
			"identifier": "username", "password": true, "verificationCode": false, "isPasswordPrimary": true,
		}}},
		"unknownSessionRedirectUrl": nil,
	})
	requireValid(t, "GetSignInExp response fields", p, err)
}
