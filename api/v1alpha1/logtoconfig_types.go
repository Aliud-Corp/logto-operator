package v1alpha1

import (
	"bytes"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LogtoConfig is the desired configuration of one Logto instance.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=logtocfg
// +kubebuilder:printcolumn:name="Mode",type=string,JSONPath=`.spec.mode`
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Drifted",type=string,JSONPath=`.status.conditions[?(@.type=="Drifted")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type LogtoConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   LogtoConfigSpec   `json:"spec"`
	Status LogtoConfigStatus `json:"status,omitempty"`
}

// LogtoConfigList is a list of LogtoConfig.
//
// +kubebuilder:object:root=true
type LogtoConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []LogtoConfig `json:"items"`
}

// LogtoConfigSpec declares what the tool owns in one Logto tenant. Objects that reference each other are maps keyed
// by a local key (stable, ours), distinct from the Logto name (theirs, shown in the console).
type LogtoConfigSpec struct {
	// Endpoint is where the Management API answers (the tenant endpoint).
	// +kubebuilder:validation:Pattern=`^https?://`
	Endpoint string `json:"endpoint"`

	// Credentials of the M2M application the tool authenticates as.
	Credentials Credentials `json:"credentials"`

	// Mode is what the operator does on each reconcile: Observe plans and reports drift (status Drifted), Apply
	// reconciles Logto. logtoctl ignores it: its verb decides. Every environment starts in Observe.
	// +optional
	// +kubebuilder:default=Observe
	// +kubebuilder:validation:Enum=Observe;Apply
	Mode string `json:"mode,omitempty"`

	// ResyncInterval is how often the operator reconciles an unchanged document (default 10m).
	// +optional
	ResyncInterval *metav1.Duration `json:"resyncInterval,omitempty"`

	// AllowUntestedLogto lets the operator run against a Logto whose spec is none of the supported versions
	// (logtoctl's --allow-untested-logto).
	// +optional
	AllowUntestedLogto bool `json:"allowUntestedLogto,omitempty"`

	// Outputs names where the operator publishes what Logto generates, in the document's namespace: identifiers in
	// a ConfigMap, secrets in a Secret, both owned by the LogtoConfig. logtoctl ignores it (--outputs, --secrets-out).
	// +optional
	Outputs *Outputs `json:"outputs,omitempty"`

	// Variables usable as ${name} in string fields of this spec.
	// +optional
	Variables map[string]string `json:"variables,omitempty"`

	// Prune, per kind, deletes the Logto objects of that kind that this document does not declare. Off by default.
	// +optional
	Prune Prune `json:"prune,omitempty"`

	// +optional
	Connectors map[string]Connector `json:"connectors,omitempty"`
	// +optional
	CustomProfileFields map[string]CustomProfileField `json:"customProfileFields,omitempty"`
	// +optional
	SignInExperience *SignInExperience `json:"signInExperience,omitempty"`
	// +optional
	Resources map[string]Resource `json:"resources,omitempty"`
	// +optional
	OrganizationScopes map[string]OrganizationScope `json:"organizationScopes,omitempty"`
	// +optional
	OrganizationRoles map[string]OrganizationRole `json:"organizationRoles,omitempty"`
	// Roles are global roles (role-based access control of the tenant), granted to applications.
	// +optional
	Roles map[string]Role `json:"roles,omitempty"`
	// +optional
	Applications map[string]Application `json:"applications,omitempty"`
	// CIMD is the tenant's Client ID Metadata Document setting (Logto 1.44 and later): whether a client may identify
	// itself with the URL of a metadata document, and the scopes its consent screen may grant. Absent, it is not
	// managed at all.
	// +optional
	CIMD *CIMD `json:"cimd,omitempty"`
}

// Credentials of the Management API client.
type Credentials struct {
	ClientID     StringSource `json:"clientId"`
	ClientSecret SecretSource `json:"clientSecret"`
	// TokenEndpoint is where the client gets its token, when it is not the tenant endpoint (the m-default route uses
	// the console endpoint).
	// +optional
	// +kubebuilder:validation:Pattern=`^https?://`
	TokenEndpoint string `json:"tokenEndpoint,omitempty"`
}

// StringSource is a value written in the document, or read from the environment, a file or a Secret.
type StringSource struct {
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	ValueFrom *ValueFrom `json:"valueFrom,omitempty"`
}

// SecretSource is a secret: it is never written in the document.
type SecretSource struct {
	ValueFrom ValueFrom `json:"valueFrom"`
}

// ValueFrom names exactly one place a value is read from. The CLI reads env and file; the operator reads
// secretKeyRef.
type ValueFrom struct {
	// +optional
	Env string `json:"env,omitempty"`
	// +optional
	File string `json:"file,omitempty"`
	// +optional
	SecretKeyRef *SecretKeyRef `json:"secretKeyRef,omitempty"`
}

// SecretKeyRef points at one key of a Kubernetes Secret in the namespace of the LogtoConfig.
type SecretKeyRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// TextSource is text written in the document, or read from a file relative to the document.
type TextSource struct {
	// +optional
	Value string `json:"value,omitempty"`
	// +optional
	FromFile string `json:"fromFile,omitempty"`
	// As is how a file is turned into the value: text (default) or dataUri (images).
	// +optional
	// +kubebuilder:validation:Enum=text;dataUri
	As string `json:"as,omitempty"`
}

// Prune switches, one per kind. Users and organizations are never pruned.
type Prune struct {
	// +optional
	Connectors bool `json:"connectors,omitempty"`
	// +optional
	CustomProfileFields bool `json:"customProfileFields,omitempty"`
	// +optional
	Resources bool `json:"resources,omitempty"`
	// +optional
	OrganizationScopes bool `json:"organizationScopes,omitempty"`
	// +optional
	OrganizationRoles bool `json:"organizationRoles,omitempty"`
	// Roles prunes global roles; a role holding a scope of Logto's Management API is never pruned.
	// +optional
	Roles bool `json:"roles,omitempty"`
	// Keep lists objects never pruned, whatever the switches say: by name (organization scope and role, global role,
	// custom profile field), by name or indicator (resource), by id (connector, which has no name of its own).
	// +optional
	Keep []string `json:"keep,omitempty"`
}

// Connector is a passwordless connector. M1 supports the SMTP e-mail connector.
type Connector struct {
	// ID is the connector id in Logto (declared: Logto accepts it at creation).
	// +optional
	ID string `json:"id,omitempty"`
	// ConnectorID is the factory id, e.g. simple-mail-transfer-protocol.
	ConnectorID string `json:"connectorId"`
	// +optional
	SMTP *SMTPConfig `json:"smtp,omitempty"`
}

// SMTPConfig is the config of the simple-mail-transfer-protocol connector.
type SMTPConfig struct {
	Host string `json:"host"`
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`
	// +optional
	Secure bool `json:"secure,omitempty"`
	// +optional
	IgnoreTLS bool     `json:"ignoreTLS,omitempty"`
	Auth      SMTPAuth `json:"auth"`
	FromEmail string   `json:"fromEmail"`
	// +optional
	ReplyTo string `json:"replyTo,omitempty"`
	// Templates are listed, or read from a directory of <usageType>.json files.
	Templates EmailTemplates `json:"templates"`
}

// SMTPAuth are the SMTP credentials; both are required by the connector.
type SMTPAuth struct {
	User StringSource `json:"user"`
	Pass SecretSource `json:"pass"`
}

// EmailTemplates are inline items, or a directory relative to the document, one JSON template per file, whose
// content.fromFile is relative to that directory.
type EmailTemplates struct {
	// +optional
	FromDir string `json:"fromDir,omitempty"`
	// +optional
	Items []EmailTemplate `json:"items,omitempty"`
}

// EmailTemplate is one template of the SMTP connector.
type EmailTemplate struct {
	UsageType string `json:"usageType"`
	// +kubebuilder:validation:Enum=text/plain;text/html
	ContentType string     `json:"contentType"`
	Subject     string     `json:"subject"`
	Content     TextSource `json:"content"`
}

// CustomProfileField is a sign-up profile field, matched by its name.
type CustomProfileField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Label string `json:"label"`
	// +optional
	Description string `json:"description,omitempty"`
	// +optional
	Required bool `json:"required,omitempty"`
	// Config is passed to Logto as is (its shape depends on the type).
	// +optional
	Config *Free `json:"config,omitempty"`
}

// SignInExperience lists the owned keys of the sign-in experience; undeclared keys are left as Logto has them.
type SignInExperience struct {
	// +optional
	Color *Color `json:"color,omitempty"`
	// +optional
	LanguageInfo *LanguageInfo `json:"languageInfo,omitempty"`
	// +optional
	Branding *Branding `json:"branding,omitempty"`
	// +optional
	CustomCSS *TextSource `json:"customCss,omitempty"`
	// +optional
	SignUp *SignUp `json:"signUp,omitempty"`
	// +optional
	SignIn *SignIn `json:"signIn,omitempty"`
	// +optional
	UnknownSessionRedirectURL string `json:"unknownSessionRedirectUrl,omitempty"`
}

// Color of the sign-in pages.
type Color struct {
	// +kubebuilder:validation:Pattern=`^#([0-9A-Fa-f]{3}){1,2}$`
	PrimaryColor string `json:"primaryColor"`
	// +optional
	IsDarkModeEnabled bool `json:"isDarkModeEnabled,omitempty"`
	// +optional
	// +kubebuilder:validation:Pattern=`^#([0-9A-Fa-f]{3}){1,2}$`
	DarkPrimaryColor string `json:"darkPrimaryColor,omitempty"`
}

// LanguageInfo of the sign-in pages.
type LanguageInfo struct {
	// +optional
	AutoDetect bool `json:"autoDetect,omitempty"`
	// +optional
	FallbackLanguage string `json:"fallbackLanguage,omitempty"`
}

// Branding of the sign-in pages: URLs, or files turned into data URIs.
type Branding struct {
	// +optional
	LogoURL *TextSource `json:"logoUrl,omitempty"`
	// +optional
	DarkLogoURL *TextSource `json:"darkLogoUrl,omitempty"`
	// +optional
	Favicon *TextSource `json:"favicon,omitempty"`
}

// SignUp settings.
type SignUp struct {
	Identifiers []string `json:"identifiers"`
	// +optional
	Password bool `json:"password,omitempty"`
	// +optional
	Verify bool `json:"verify,omitempty"`
}

// SignIn settings.
type SignIn struct {
	Methods []SignInMethod `json:"methods"`
}

// SignInMethod is one sign-in method, in order.
type SignInMethod struct {
	Identifier string `json:"identifier"`
	// +optional
	Password bool `json:"password,omitempty"`
	// +optional
	VerificationCode bool `json:"verificationCode,omitempty"`
	// +optional
	IsPasswordPrimary bool `json:"isPasswordPrimary,omitempty"`
}

// Resource is an API resource, matched by its indicator.
type Resource struct {
	Name string `json:"name"`
	// +kubebuilder:validation:Pattern=`^https?://`
	Indicator string `json:"indicator"`
	// +optional
	// +kubebuilder:validation:Minimum=1
	AccessTokenTTL int32 `json:"accessTokenTtl,omitempty"`
	// Scopes by name.
	// +optional
	Scopes map[string]Scope `json:"scopes,omitempty"`
}

// Scope of an API resource or of the organization template.
type Scope struct {
	// +optional
	Description string `json:"description,omitempty"`
}

// OrganizationScope is a scope of the organization template, keyed by its name.
type OrganizationScope = Scope

// OrganizationRole is a role of the organization template, matched by its name.
type OrganizationRole struct {
	Name string `json:"name"`
	// +optional
	Description string `json:"description,omitempty"`
	// OrganizationScopes are names declared in spec.organizationScopes; the role holds exactly these.
	// +optional
	OrganizationScopes []string `json:"organizationScopes,omitempty"`
	// ResourceScopes are scopes of declared resources; the role holds exactly these.
	// +optional
	ResourceScopes []ResourceScopeRef `json:"resourceScopes,omitempty"`
}

// Role is a global role, matched by its name. Its type cannot change once it exists in Logto.
type Role struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Name string `json:"name"`
	// Description is required by Logto.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	Description string `json:"description"`
	// +kubebuilder:validation:Enum=User;MachineToMachine
	Type string `json:"type"`
	// IsDefault makes Logto grant the role to every new user (User roles only).
	// +optional
	IsDefault *bool `json:"isDefault,omitempty"`
	// Scopes are scopes of declared resources. When set, the role holds exactly these (an empty list removes them
	// all); when omitted, the scopes Logto holds are left as they are.
	// +optional
	Scopes *[]ResourceScopeRef `json:"scopes,omitempty"`
}

// ResourceScopeRef names a scope of a resource declared in spec.resources, by local key and scope name.
type ResourceScopeRef struct {
	Resource string `json:"resource"`
	Scope    string `json:"scope"`
}

// Application is matched by its Logto name and never deleted or recreated.
type Application struct {
	Name string `json:"name"`
	// Type cannot change once the application exists in Logto.
	// +kubebuilder:validation:Enum=Native;SPA;Traditional;MachineToMachine
	Type string `json:"type"`
	// +optional
	Description string `json:"description,omitempty"`
	// IsThirdParty makes the application a third-party one: its users go through a consent screen, and its tokens
	// are capped to the consent scopes. Logto sets it at creation only: like the type, it cannot change once the
	// application exists. Applies to a Native, SPA or Traditional application.
	// +optional
	IsThirdParty *bool `json:"isThirdParty,omitempty"`
	// ConsentScopes are the scopes the consent screen of a third-party application may grant.
	// +optional
	ConsentScopes *ConsentScopes `json:"consentScopes,omitempty"`
	// +optional
	OIDCClientMetadata *OIDCClientMetadata `json:"oidcClientMetadata,omitempty"`
	// +optional
	CustomClientMetadata *CustomClientMetadata `json:"customClientMetadata,omitempty"`
	// Roles granted to a MachineToMachine application, by Logto name (e.g. "Logto Management API access").
	// +optional
	Roles []RoleRef `json:"roles,omitempty"`
	// Secret of a MachineToMachine application: one usable secret is ensured and published, never rotated.
	// +optional
	Secret *ApplicationSecret `json:"secret,omitempty"`
}

// CIMD is Logto's Client ID Metadata Document setting. Turning it on needs Logto's outbound SSRF protection to be on
// with no allowlist (Logto refuses it otherwise, and the run fails with that refusal).
type CIMD struct {
	// Enabled turns CIMD on or off for the tenant.
	Enabled bool `json:"enabled"`
	// AddConsentPromptForOfflineAccess is Logto's experimental switch that adds `consent` to the prompt of CIMD
	// authorization requests asking for offline access. Omitted, it is left as Logto has it.
	// +optional
	AddConsentPromptForOfflineAccess *bool `json:"addConsentPromptForOfflineAccess,omitempty"`
	// ConsentScopes is the tenant-wide ceiling of what the consent screen of a CIMD client may grant; the same lists
	// as an application's, with the same rules.
	// +optional
	ConsentScopes *ConsentScopes `json:"consentScopes,omitempty"`
}

// ConsentScopes of a consent screen (a third-party application's, or the tenant's CIMD clients'). A list that is
// declared is exact (the screen holds these scopes and no other of its kind: an empty list removes them all); a list
// that is omitted leaves what Logto holds.
// Organization scopes are not managed yet.
type ConsentScopes struct {
	// Resources are scopes of declared resources.
	// +optional
	Resources *[]ResourceScopeRef `json:"resources,omitempty"`
	// User are user scopes: the claims of the user, by Logto's own names.
	// +optional
	User *[]ConsentUserScope `json:"user,omitempty"`
}

// ConsentUserScope is a user scope Logto accepts in a consent screen. personal_access_tokens needs Logto 1.46.
// +kubebuilder:validation:Enum=profile;email;phone;address;custom_data;identities;roles;"urn:logto:scope:organizations";"urn:logto:scope:organization_roles";"urn:logto:scope:sessions";"urn:logto:scope:trusted_devices";"urn:logto:scope:personal_access_tokens"
type ConsentUserScope string

// OIDCClientMetadata of an application; URI lists are compared as sets.
type OIDCClientMetadata struct {
	// +optional
	RedirectURIs []string `json:"redirectUris,omitempty"`
	// +optional
	PostLogoutRedirectURIs []string `json:"postLogoutRedirectUris,omitempty"`
}

// CustomClientMetadata of an application; undeclared keys are left as Logto has them.
type CustomClientMetadata struct {
	// +optional
	CORSAllowedOrigins []string `json:"corsAllowedOrigins,omitempty"`
	// +optional
	AlwaysIssueRefreshToken *bool `json:"alwaysIssueRefreshToken,omitempty"`
	// +optional
	RotateRefreshToken *bool `json:"rotateRefreshToken,omitempty"`
	// +optional
	IDTokenTTL *int32 `json:"idTokenTtl,omitempty"`
	// +optional
	RefreshTokenTTLInDays *int32 `json:"refreshTokenTtlInDays,omitempty"`
	// +optional
	AllowTokenExchange *bool `json:"allowTokenExchange,omitempty"`
}

// ApplicationSecret asks for the M2M secret to be published as an output.
type ApplicationSecret struct {
	Publish bool `json:"publish"`
}

// RoleRef names an existing Logto global role by name.
type RoleRef struct {
	Name string `json:"name"`
}

// Outputs of the operator.
type Outputs struct {
	// ConfigMap receives the identifiers (application ids, resource indicators), one key per output.
	// +optional
	ConfigMap *OutputTarget `json:"configMap,omitempty"`
	// Secret receives the secrets (M2M application secrets), one key per output.
	// +optional
	Secret *OutputTarget `json:"secret,omitempty"`
}

// OutputTarget names an object of the document's namespace.
type OutputTarget struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=253
	Name string `json:"name"`
	// Keys, when set, publishes only these outputs, each under the key it is mapped from (LOGTO_APP_ID:
	// applications.organizer.id), so that a Pod can take the object whole with envFrom. Without keys, every output
	// is published under its own name (applications.<key>.id, applications.<key>.secret, resources.<key>.indicator).
	// +optional
	Keys map[string]string `json:"keys,omitempty"`
}

// Modes of the operator.
const (
	ModeObserve = "Observe"
	ModeApply   = "Apply"
)

// LogtoConfigStatus is written by the operator.
type LogtoConfigStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions: Ready (the last reconcile succeeded and Logto matches the document), Drifted (Logto differs from
	// the document; Observe mode only reports it), Degraded (the document or the target is in error until it changes).
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// LogtoVersion is the supported version the target's spec matched, empty when untested.
	// +optional
	LogtoVersion string `json:"logtoVersion,omitempty"`
	// LastAppliedTime is when an apply last wrote to Logto.
	// +optional
	LastAppliedTime *metav1.Time `json:"lastAppliedTime,omitempty"`
}

// Free is a JSON object passed to Logto as is.
//
// +kubebuilder:validation:Type=object
// +kubebuilder:pruning:PreserveUnknownFields
// +kubebuilder:validation:Schemaless
type Free struct {
	Raw json.RawMessage `json:"-"`
}

// MarshalJSON writes the raw object.
func (f Free) MarshalJSON() ([]byte, error) {
	if len(f.Raw) == 0 {
		return []byte("null"), nil
	}
	return f.Raw, nil
}

// UnmarshalJSON keeps the raw object, refusing anything that is not an object.
func (f *Free) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if !bytes.HasPrefix(trimmed, []byte("{")) && !bytes.Equal(trimmed, []byte("null")) {
		return &json.UnmarshalTypeError{Value: string(trimmed[:min(len(trimmed), 20)]), Type: nil}
	}
	f.Raw = append(f.Raw[:0], trimmed...)
	return nil
}
