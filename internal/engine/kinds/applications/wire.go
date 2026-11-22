package applications

import "github.com/Aliud-Corp/logto-operator/api/v1alpha1"

// The kind's own wire types. Metadata objects are maps so that the keys the document does not declare are sent back
// as Logto holds them (a PATCH of oidcClientMetadata replaces the object).

type wireApp struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description *string        `json:"description"`
	OIDC        map[string]any `json:"oidcClientMetadata"`
	Custom      map[string]any `json:"customClientMetadata"`
}

type createApp struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Description string         `json:"description,omitempty"`
	OIDC        map[string]any `json:"oidcClientMetadata"`
	Custom      map[string]any `json:"customClientMetadata,omitempty"`
}

type updateApp struct {
	Description string         `json:"description,omitempty"`
	OIDC        map[string]any `json:"oidcClientMetadata,omitempty"`
	Custom      map[string]any `json:"customClientMetadata,omitempty"`
}

type wireSecret struct {
	Name      string   `json:"name"`
	Value     string   `json:"value"`
	ExpiresAt *float64 `json:"expiresAt"`
}

type createSecret struct {
	Name string `json:"name"`
}

type wireRole struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type assignRoles struct {
	RoleIDs []string `json:"roleIds"`
}

func oidc(d v1alpha1.Application, have map[string]any) map[string]any {
	out := map[string]any{"redirectUris": []string{}, "postLogoutRedirectUris": []string{}}
	for k, v := range have {
		out[k] = v
	}
	if m := d.OIDCClientMetadata; m != nil {
		out["redirectUris"] = orEmpty(m.RedirectURIs)
		out["postLogoutRedirectUris"] = orEmpty(m.PostLogoutRedirectURIs)
	}
	return out
}

func orEmpty(l []string) []string {
	if l == nil {
		return []string{}
	}
	return l
}

func newCreate(d v1alpha1.Application) createApp {
	return createApp{Name: d.Name, Type: d.Type, Description: d.Description, OIDC: oidc(d, nil), Custom: customMap(d)}
}

func newUpdate(d v1alpha1.Application, have wireApp) *updateApp {
	u := &updateApp{Description: d.Description, OIDC: oidc(d, have.OIDC)}
	if want := customMap(d); len(want) > 0 {
		u.Custom = map[string]any{}
		for k, v := range have.Custom {
			u.Custom[k] = v
		}
		for k, v := range want {
			u.Custom[k] = v
		}
	}
	return u
}
