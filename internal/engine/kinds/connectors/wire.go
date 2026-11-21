package connectors

// The kind's own wire types. The config is sent to the connector, which validates it whole; the password travels in
// the body only (the client never logs nor wraps a body) and is masked in every plan.

type wireConnector struct {
	ID          string         `json:"id"`
	ConnectorID string         `json:"connectorId"`
	Config      map[string]any `json:"config"`
}

type smtpAuth struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

type smtpTemplate struct {
	UsageType   string `json:"usageType"`
	ContentType string `json:"contentType"`
	Subject     string `json:"subject"`
	Content     string `json:"content"`
}

type smtpConfig struct {
	Host      string         `json:"host"`
	Port      int            `json:"port"`
	Secure    bool           `json:"secure"`
	IgnoreTLS bool           `json:"ignoreTLS,omitempty"`
	Auth      smtpAuth       `json:"auth"`
	FromEmail string         `json:"fromEmail"`
	ReplyTo   string         `json:"replyTo,omitempty"`
	Templates []smtpTemplate `json:"templates"`
}

type createConnector struct {
	ID          string     `json:"id,omitempty"`
	ConnectorID string     `json:"connectorId"`
	Config      smtpConfig `json:"config"`
}

type updateConnector struct {
	Config smtpConfig `json:"config"`
}
