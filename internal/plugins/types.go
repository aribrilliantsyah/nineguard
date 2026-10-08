package plugins

import "time"

type ScopeType string

const (
	ScopeGlobal ScopeType = "global"
	ScopeGroup  ScopeType = "group"
	ScopeKey    ScopeType = "key"
)

type BindingState string

const (
	StateOn      BindingState = "on"
	StateOff     BindingState = "off"
	StateInherit BindingState = "inherit"
)

type Category string

const (
	CategoryInputCompression Category = "input_compression"
	CategoryOutputStyle      Category = "output_style"
	CategoryOther            Category = "other"
)

type FailurePolicy string

const (
	PolicyOpen   FailurePolicy = "open"
	PolicyClosed FailurePolicy = "closed"
)

type PluginKind string

const (
	KindBuiltin PluginKind = "builtin"
	KindHTTP    PluginKind = "http"
)

type Plugin struct {
	ID              string        `json:"id"`
	Kind            PluginKind    `json:"kind"`
	Name            string        `json:"name"`
	Description     string        `json:"description"`
	URL             string        `json:"url,omitempty"`
	Secret          string        `json:"secret,omitempty"`
	TimeoutMs       int           `json:"timeout_ms"`
	FailurePolicy   FailurePolicy `json:"failure_policy"`
	Bypassable      bool          `json:"bypassable"`
	PipelineOrder   int           `json:"pipeline_order"`
	Category        Category      `json:"category"`
	Summary         string        `json:"summary,omitempty"`
	DefaultSettings string        `json:"default_settings"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
}

type Binding struct {
	PluginID  string       `json:"plugin_id"`
	ScopeType ScopeType    `json:"scope_type"`
	ScopeID   string       `json:"scope_id"`
	State     BindingState `json:"state"`
	Settings  string       `json:"settings"`
	UpdatedAt time.Time    `json:"updated_at"`
}

type ScopeOrigin struct {
	ScopeType ScopeType `json:"scope_type"`
	ScopeID   string    `json:"scope_id"`
	Label     string    `json:"label"`
}

type Warning struct {
	Code     string   `json:"code"`
	Plugins  []string `json:"plugins"`
	Provider string   `json:"provider,omitempty"`
	Message  string   `json:"message"`
}

type ResolvedPlugin struct {
	Plugin         Plugin         `json:"plugin"`
	EffectiveState BindingState   `json:"effective_state"`
	MergedSettings map[string]any `json:"merged_settings"`
	DecidedBy      ScopeOrigin    `json:"decided_by"`
	Overridden     []ScopeOrigin  `json:"overridden,omitempty"`
}

type PipelineResult struct {
	Body           []byte
	PluginsApplied []string
	PluginsSkipped []string // format id:reason
	TokensSaved    int
	TokensOverhead int
	PluginErrors   []string
	DurationMs     int64
	Rejected       bool
	RejectCode     int
	RejectMessage  string
}
