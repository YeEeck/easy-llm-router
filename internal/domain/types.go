package domain

import "time"

type CredentialStatus string

const (
	StatusUnknown   CredentialStatus = "unknown"
	StatusAvailable CredentialStatus = "available"
	StatusExhausted CredentialStatus = "exhausted"
	StatusInvalid   CredentialStatus = "invalid"
	StatusDisabled  CredentialStatus = "disabled"
)

func (s CredentialStatus) Valid() bool {
	switch s {
	case StatusUnknown, StatusAvailable, StatusExhausted, StatusInvalid, StatusDisabled:
		return true
	default:
		return false
	}
}

type Classification string

const (
	ClassSuccess      Classification = "success"
	ClassExhausted    Classification = "exhausted"
	ClassInvalid      Classification = "invalid"
	ClassInconclusive Classification = "inconclusive"
)

type MatchOperator string

const (
	MatchExists MatchOperator = "exists"
	MatchEquals MatchOperator = "equals"
	MatchRegex  MatchOperator = "regex"
)

type Condition struct {
	StatusMin int           `yaml:"status_min,omitempty" json:"status_min,omitempty"`
	StatusMax int           `yaml:"status_max,omitempty" json:"status_max,omitempty"`
	Header    string        `yaml:"header,omitempty" json:"header,omitempty"`
	JSONPath  string        `yaml:"json_path,omitempty" json:"json_path,omitempty"`
	Body      bool          `yaml:"body,omitempty" json:"body,omitempty"`
	Operator  MatchOperator `yaml:"operator,omitempty" json:"operator,omitempty"`
	Value     string        `yaml:"value,omitempty" json:"value,omitempty"`
}

type ResponseRule struct {
	Name       string         `yaml:"name" json:"name"`
	Result     Classification `yaml:"result" json:"result"`
	Conditions []Condition    `yaml:"conditions" json:"conditions"`
}

type RecoveryHintSource string

const (
	RecoveryHintFromHeader RecoveryHintSource = "header"
	RecoveryHintFromJSON   RecoveryHintSource = "json"
	RecoveryHintFromBody   RecoveryHintSource = "body"
)

type RecoveryHintParse string

const (
	RecoveryHintAsDuration RecoveryHintParse = "duration"
	RecoveryHintAsAbsolute RecoveryHintParse = "absolute"
	RecoveryHintAsSeconds  RecoveryHintParse = "seconds"
)

type RecoveryHintConfig struct {
	Source   RecoveryHintSource `yaml:"source" json:"source"`
	Header   string             `yaml:"header,omitempty" json:"header,omitempty"`
	JSONPath string             `yaml:"json_path,omitempty" json:"json_path,omitempty"`
	Pattern  string             `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Parse    RecoveryHintParse  `yaml:"parse" json:"parse"`
}

type QuotaEpochConfig struct {
	Source   RecoveryHintSource `yaml:"source" json:"source"`
	Header   string             `yaml:"header,omitempty" json:"header,omitempty"`
	JSONPath string             `yaml:"json_path,omitempty" json:"json_path,omitempty"`
	Pattern  string             `yaml:"pattern,omitempty" json:"pattern,omitempty"`
}

type ProbeProtocol string

const (
	ProbeOpenAIChat      ProbeProtocol = "openai_chat"
	ProbeOpenAIResponses ProbeProtocol = "openai_responses"
	ProbeAnthropic       ProbeProtocol = "anthropic_messages"
	ProbeCustom          ProbeProtocol = "custom_http"
)

type ProbeConfig struct {
	Protocol ProbeProtocol     `yaml:"protocol" json:"protocol"`
	Model    string            `yaml:"model,omitempty" json:"model,omitempty"`
	Method   string            `yaml:"method,omitempty" json:"method,omitempty"`
	Path     string            `yaml:"path,omitempty" json:"path,omitempty"`
	Headers  map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Body     map[string]any    `yaml:"body,omitempty" json:"body,omitempty"`
}

type Service struct {
	ID           string             `yaml:"id" json:"id"`
	Name         string             `yaml:"name" json:"name"`
	Preset       string             `yaml:"preset,omitempty" json:"preset,omitempty"`
	BaseURL      string             `yaml:"base_url" json:"base_url"`
	AuthHeader   string             `yaml:"auth_header,omitempty" json:"auth_header,omitempty"`
	AuthPrefix   string             `yaml:"auth_prefix,omitempty" json:"auth_prefix,omitempty"`
	Rules        []ResponseRule     `yaml:"rules,omitempty" json:"rules,omitempty"`
	RecoveryHint RecoveryHintConfig `yaml:"recovery_hint,omitempty" json:"recovery_hint,omitempty"`
	QuotaEpoch   QuotaEpochConfig   `yaml:"quota_epoch,omitempty" json:"quota_epoch,omitempty"`
	Probe        ProbeConfig        `yaml:"probe" json:"probe"`
}

type Credential struct {
	ID              string `yaml:"id" json:"id"`
	Name            string `yaml:"name" json:"name"`
	ServiceID       string `yaml:"service" json:"service"`
	BaseURLOverride string `yaml:"base_url,omitempty" json:"base_url,omitempty"`
}

type Pool struct {
	Name           string        `yaml:"name" json:"name"`
	CredentialIDs  []string      `yaml:"credentials" json:"credentials"`
	VerifyInterval time.Duration `yaml:"verify_interval" json:"verify_interval"`
}

type Settings struct {
	Port                 int           `yaml:"port" json:"port"`
	LogLevel             string        `yaml:"log_level" json:"log_level"`
	ReplayMemoryLimit    int64         `yaml:"replay_memory_limit" json:"replay_memory_limit"`
	ReplayLimit          int64         `yaml:"replay_limit" json:"replay_limit"`
	ResponseInspectLimit int64         `yaml:"response_inspect_limit" json:"response_inspect_limit"`
	ShutdownTimeout      time.Duration `yaml:"shutdown_timeout" json:"shutdown_timeout"`
}

type Config struct {
	Version     int          `yaml:"version" json:"version"`
	Settings    Settings     `yaml:"settings" json:"settings"`
	Services    []Service    `yaml:"services" json:"services"`
	Credentials []Credential `yaml:"credentials" json:"credentials"`
	Pools       []Pool       `yaml:"pools" json:"pools"`
}

type CredentialState struct {
	Status         CredentialStatus `json:"status"`
	ChangedAt      time.Time        `json:"changed_at"`
	Reason         string           `json:"reason,omitempty"`
	NextVerifyAt   time.Time        `json:"next_verify_at,omitempty"`
	RecoveryHint   time.Time        `json:"recovery_hint,omitempty"`
	QuotaEpoch     string           `json:"quota_epoch,omitempty"`
	LastValidation string           `json:"last_validation,omitempty"`
}

type RuntimeState struct {
	Version     int                        `json:"version"`
	Credentials map[string]CredentialState `json:"credentials"`
	Current     map[string]string          `json:"current"`
}

type Secrets struct {
	Version int               `yaml:"version"`
	APIKeys map[string]string `yaml:"api_keys"`
}
