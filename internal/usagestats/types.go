package usagestats

const storageVersion = 1

// ModelPrice stores USD prices as micro-dollars per one million tokens.
type ModelPrice struct {
	Model                      string `json:"model"`
	InputMicrosPerMillion      int64  `json:"input_micros_per_million"`
	OutputMicrosPerMillion     int64  `json:"output_micros_per_million"`
	CacheReadMicrosPerMillion  int64  `json:"cache_read_micros_per_million"`
	CacheWriteMicrosPerMillion int64  `json:"cache_write_micros_per_million"`
	CacheReadConfigured        bool   `json:"cache_read_configured"`
	CacheWriteConfigured       bool   `json:"cache_write_configured"`
	Source                     string `json:"source,omitempty"`
	UpdatedAtMS                int64  `json:"updated_at_ms"`
}

// Aggregate contains all-time usage for one account, model, and service tier.
type Aggregate struct {
	AccountKey           string `json:"account_key"`
	AuthIndex            string `json:"auth_index,omitempty"`
	AccountLabel         string `json:"account_label"`
	Provider             string `json:"provider"`
	ExecutorType         string `json:"executor_type,omitempty"`
	AuthType             string `json:"auth_type,omitempty"`
	Model                string `json:"model"`
	ServiceTier          string `json:"service_tier"`
	Calls                int64  `json:"calls"`
	SuccessCalls         int64  `json:"success_calls"`
	FailureCalls         int64  `json:"failure_calls"`
	InputTokens          int64  `json:"input_tokens"`
	OutputTokens         int64  `json:"output_tokens"`
	ReasoningTokens      int64  `json:"reasoning_tokens"`
	CachedTokens         int64  `json:"cached_tokens"`
	CacheReadTokens      int64  `json:"cache_read_tokens"`
	CacheWriteTokens     int64  `json:"cache_write_tokens"`
	TotalTokens          int64  `json:"total_tokens"`
	LongInputTokens      int64  `json:"long_input_tokens,omitempty"`
	LongOutputTokens     int64  `json:"long_output_tokens,omitempty"`
	LongCachedTokens     int64  `json:"long_cached_tokens,omitempty"`
	LongCacheReadTokens  int64  `json:"long_cache_read_tokens,omitempty"`
	LongCacheWriteTokens int64  `json:"long_cache_write_tokens,omitempty"`
	FirstSeenMS          int64  `json:"first_seen_ms"`
	LastSeenMS           int64  `json:"last_seen_ms"`
}

type persistentState struct {
	Version    int                   `json:"version"`
	Prices     map[string]ModelPrice `json:"prices"`
	Aggregates map[string]Aggregate  `json:"aggregates"`
}

// Totals contains counters shared by account and model summaries.
type Totals struct {
	Calls            int64 `json:"calls"`
	SuccessCalls     int64 `json:"success_calls"`
	FailureCalls     int64 `json:"failure_calls"`
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens"`
	CacheReadTokens  int64 `json:"cache_read_tokens"`
	CacheWriteTokens int64 `json:"cache_write_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	CostMicros       int64 `json:"cost_micros"`
	UnpricedCalls    int64 `json:"unpriced_calls"`
}

// ModelUsageSummary describes usage and estimated cost for one model.
type ModelUsageSummary struct {
	Model  string `json:"model"`
	Priced bool   `json:"priced"`
	Totals
}

// AccountSummary describes usage and estimated cost for one selected credential.
type AccountSummary struct {
	AccountKey   string              `json:"account_key"`
	AuthIndex    string              `json:"auth_index,omitempty"`
	AccountLabel string              `json:"account_label"`
	Provider     string              `json:"provider"`
	AuthType     string              `json:"auth_type,omitempty"`
	FirstSeenMS  int64               `json:"first_seen_ms"`
	LastSeenMS   int64               `json:"last_seen_ms"`
	Models       []ModelUsageSummary `json:"models"`
	Totals
}

// Summary is the Management API response for persisted usage cost analytics.
type Summary struct {
	Enabled        bool                `json:"enabled"`
	UpdatedAtMS    int64               `json:"updated_at_ms"`
	Accounts       []AccountSummary    `json:"accounts"`
	Models         []ModelUsageSummary `json:"models"`
	UnpricedModels []string            `json:"unpriced_models"`
	Totals
}
