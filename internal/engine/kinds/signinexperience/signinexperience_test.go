package signinexperience_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/connectors"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/signinexperience"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

const smtp = `  connectors:
    smtp:
      id: example-smtp
      connectorId: simple-mail-transfer-protocol
      smtp:
        host: smtp.example.test
        port: 587
        auth: {user: {value: mailer}, pass: {valueFrom: {env: TEST_SMTP_PASS}}}
        fromEmail: no-reply@example.test
        templates:
          items:
            - {usageType: Register, contentType: text/plain, subject: s, content: {value: c}}
            - {usageType: SignIn, contentType: text/plain, subject: s, content: {value: c}}
            - {usageType: ForgotPassword, contentType: text/plain, subject: s, content: {value: c}}
            - {usageType: Generic, contentType: text/plain, subject: s, content: {value: c}}
`

const sie = `  signInExperience:
    color: {primaryColor: "#5B4FE0"}
    languageInfo: {autoDetect: false, fallbackLanguage: fr}
    customCss: {value: ".logto_main-content { border-radius: 12px; }"}
    signUp: {identifiers: [email], password: true, verify: true}
    signIn:
      methods:
        - {identifier: email, password: true, verificationCode: true, isPasswordPrimary: true}
    unknownSessionRedirectUrl: https://app.example.test
`

var kinds = []engine.Kind{connectors.Kind{}, signinexperience.Kind{}}

func TestTheDeclaredKeysAreAppliedAndAReplayWritesNothing(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "pass-value")
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, smtp+sie)
	kindtest.Apply(t, c, spec, kinds...)
	got := fake.SignInExperience()
	col := got["color"].(map[string]any)
	if col["primaryColor"] != "#5B4FE0" || col["darkPrimaryColor"] != "#8C6CFF" {
		t.Fatalf("color = %v (the undeclared dark colour must come from Logto)", col)
	}
	if got["languageInfo"].(map[string]any)["fallbackLanguage"] != "fr" || got["unknownSessionRedirectUrl"] != "https://app.example.test" {
		t.Fatalf("sign-in experience = %v", got)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, spec, kinds...))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func TestAnUndeclaredKeyChangedByHandIsLeftAlone(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "pass-value")
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, smtp+sie), kinds...)
	req, _ := mgmt.NewUpdateSignInExpRequestWithBody(c.Endpoint(), &mgmt.UpdateSignInExpParams{}, "application/json", strings.NewReader(`{"signInMode":"SignIn"}`))
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, kindtest.Apply(t, c, kindtest.Spec(t, smtp+sie), kinds...))
	if fake.SignInExperience()["signInMode"] != "SignIn" {
		t.Fatal("an undeclared key was overwritten")
	}
}

func TestAnEmailIdentifierWithoutAConnectorIsRefusedBeforeAnyWrite(t *testing.T) {
	fake, c := kindtest.Fake(t)
	_, err := kindtest.Plan(c, kindtest.Spec(t, sie), signinexperience.Kind{}, connectors.Kind{})
	if err == nil || !strings.Contains(err.Error(), "an e-mail identifier with verify needs an e-mail connector") {
		t.Fatalf("err = %v", err)
	}
	if fake.Writes() != 0 {
		t.Fatalf("%d writes before the refusal", fake.Writes())
	}
}
