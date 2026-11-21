package signinexperience

import "github.com/Aliud-Corp/logto-operator/api/v1alpha1"

// The kind's own wire types. Logto's PATCH requires color, languageInfo, signUp and each sign-in method whole (read
// in the spec), which is why their fields carry no omitempty.

type color struct {
	PrimaryColor      string `json:"primaryColor"`
	IsDarkModeEnabled bool   `json:"isDarkModeEnabled"`
	DarkPrimaryColor  string `json:"darkPrimaryColor"`
}

type languageInfo struct {
	AutoDetect       bool   `json:"autoDetect"`
	FallbackLanguage string `json:"fallbackLanguage"`
}

type branding struct {
	LogoURL     string `json:"logoUrl,omitempty"`
	DarkLogoURL string `json:"darkLogoUrl,omitempty"`
	Favicon     string `json:"favicon,omitempty"`
}

type signUp struct {
	Identifiers []string `json:"identifiers"`
	Password    bool     `json:"password"`
	Verify      bool     `json:"verify"`
}

type signInMethod struct {
	Identifier        string `json:"identifier"`
	Password          bool   `json:"password"`
	VerificationCode  bool   `json:"verificationCode"`
	IsPasswordPrimary bool   `json:"isPasswordPrimary"`
}

type signIn struct {
	Methods []signInMethod `json:"methods"`
}

// wireSIE is what the kind reads from GET /api/sign-in-exp, and also the desired value of the declared keys.
type wireSIE struct {
	Color                     *color        `json:"color,omitempty"`
	LanguageInfo              *languageInfo `json:"languageInfo,omitempty"`
	Branding                  *branding     `json:"branding,omitempty"`
	CustomCSS                 *string       `json:"customCss,omitempty"`
	SignUp                    *signUp       `json:"signUp,omitempty"`
	SignIn                    *signIn       `json:"signIn,omitempty"`
	UnknownSessionRedirectURL *string       `json:"unknownSessionRedirectUrl,omitempty"`
}

// desired builds the declared keys, the parts the document leaves out taken from what Logto holds.
func desired(d *v1alpha1.SignInExperience, have wireSIE) wireSIE {
	var w wireSIE
	if d.Color != nil {
		c := color{PrimaryColor: d.Color.PrimaryColor, IsDarkModeEnabled: d.Color.IsDarkModeEnabled, DarkPrimaryColor: d.Color.DarkPrimaryColor}
		if c.DarkPrimaryColor == "" {
			c.DarkPrimaryColor = c.PrimaryColor
			if have.Color != nil {
				c.DarkPrimaryColor = have.Color.DarkPrimaryColor
			}
		}
		w.Color = &c
	}
	if d.LanguageInfo != nil {
		l := languageInfo{AutoDetect: d.LanguageInfo.AutoDetect, FallbackLanguage: d.LanguageInfo.FallbackLanguage}
		if l.FallbackLanguage == "" {
			l.FallbackLanguage = "en"
			if have.LanguageInfo != nil {
				l.FallbackLanguage = have.LanguageInfo.FallbackLanguage
			}
		}
		w.LanguageInfo = &l
	}
	if b := d.Branding; b != nil {
		w.Branding = &branding{LogoURL: text(b.LogoURL), DarkLogoURL: text(b.DarkLogoURL), Favicon: text(b.Favicon)}
	}
	if d.CustomCSS != nil {
		css := d.CustomCSS.Value
		w.CustomCSS = &css
	}
	if su := d.SignUp; su != nil {
		w.SignUp = &signUp{Identifiers: append([]string{}, su.Identifiers...), Password: su.Password, Verify: su.Verify}
	}
	if si := d.SignIn; si != nil {
		w.SignIn = &signIn{Methods: []signInMethod{}}
		for _, m := range si.Methods {
			w.SignIn.Methods = append(w.SignIn.Methods, signInMethod{
				Identifier: m.Identifier, Password: m.Password, VerificationCode: m.VerificationCode, IsPasswordPrimary: m.IsPasswordPrimary,
			})
		}
	}
	if d.UnknownSessionRedirectURL != "" {
		u := d.UnknownSessionRedirectURL
		w.UnknownSessionRedirectURL = &u
	}
	return w
}

func text(t *v1alpha1.TextSource) string {
	if t == nil {
		return ""
	}
	return t.Value
}
