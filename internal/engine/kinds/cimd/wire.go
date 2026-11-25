package cimd

// wireConfig is what GET and PATCH /api/configs/cimd carry. The offline-access switch is optional in both
// directions: Logto may leave it out of an answer (not set), and the kind sends it only when the document declares it.
type wireConfig struct {
	Enabled                          bool  `json:"enabled"`
	AddConsentPromptForOfflineAccess *bool `json:"addConsentPromptForOfflineAccess,omitempty"`
}
