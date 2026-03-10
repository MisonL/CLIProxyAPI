package platform

import "time"

type RuntimeStatus struct {
	Enabled        bool      `json:"enabled"`
	Role           string    `json:"role"`
	DatabaseSchema string    `json:"database_schema"`
	TenantSlug     string    `json:"tenant_slug"`
	WorkspaceSlug  string    `json:"workspace_slug"`
	NATSStream     string    `json:"nats_stream"`
	CachePrefix    string    `json:"cache_prefix"`
	ConnectedAt    time.Time `json:"connected_at"`
}

type HistogramDataset struct {
	ID               string                  `json:"id"`
	Label            string                  `json:"label"`
	Color            string                  `json:"color"`
	Counts           []int64                 `json:"counts"`
	AverageRemaining *float64                `json:"average_remaining,omitempty"`
	BucketItems      [][]HistogramBucketItem `json:"bucket_items,omitempty"`
}

type HistogramBucketItem struct {
	CredentialID     string     `json:"credential_id"`
	FileName         string     `json:"credential_name"`
	RemainingPercent float64    `json:"remaining_percent"`
	ResetAt          *time.Time `json:"reset_at,omitempty"`
}

type HistogramBucketItemRow struct {
	CredentialID     string     `json:"credential_id"`
	CredentialName   string     `json:"credential_name"`
	RemainingPercent float64    `json:"remaining_percent"`
	ResetAt          *time.Time `json:"reset_at,omitempty"`
	Disabled         bool       `json:"disabled"`
	Unavailable      bool       `json:"unavailable"`
	QuotaExceeded    bool       `json:"quota_exceeded"`
}

type HistogramBucketItemsResponse struct {
	Provider    string                   `json:"provider"`
	DatasetID   string                   `json:"dataset_id"`
	BucketIndex int                      `json:"bucket_index"`
	Total       int64                    `json:"total"`
	Page        int                      `json:"page"`
	PageSize    int                      `json:"page_size"`
	Items       []HistogramBucketItemRow `json:"items"`
	GeneratedAt time.Time                `json:"generated_at"`
}

type WindowStat struct {
	ID                    string  `json:"id"`
	Label                 string  `json:"label"`
	RequestCount          int64   `json:"request_count"`
	TokenCount            int64   `json:"token_count"`
	FailureCount          int64   `json:"failure_count"`
	FailureRate           float64 `json:"failure_rate"`
	ActiveCredentialCount int64   `json:"active_credential_count"`
	ActivePoolPercent     float64 `json:"active_pool_percent"`
	AvgDailyRequests      float64 `json:"avg_daily_requests"`
	AvgDailyTokens        float64 `json:"avg_daily_tokens"`
}

type ProviderWarning struct {
	ID      string `json:"id"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type ProviderOverview struct {
	Provider                 string             `json:"provider"`
	Mode                     string             `json:"mode"`
	TotalCredentials         int64              `json:"total_credentials"`
	ActiveCredentials        int64              `json:"active_credentials"`
	DisabledCredentials      int64              `json:"disabled_credentials"`
	UnavailableCredentials   int64              `json:"unavailable_credentials"`
	LoadedCredentials        int64              `json:"loaded_credentials"`
	FailedQuotaCredentials   int64              `json:"failed_quota_credentials"`
	HistogramLabels          []string           `json:"histogram_labels"`
	HistogramDatasets        []HistogramDataset `json:"histogram_datasets"`
	WindowStats              []WindowStat       `json:"window_stats"`
	ConservativeHealth       *float64           `json:"conservative_health,omitempty"`
	AverageHealth            *float64           `json:"average_health,omitempty"`
	OperationalHealth        *float64           `json:"operational_health,omitempty"`
	ConservativeRiskDays     *float64           `json:"conservative_risk_days,omitempty"`
	AverageRiskDays          *float64           `json:"average_risk_days,omitempty"`
	AvgDailyQuotaBurnPercent *float64           `json:"avg_daily_quota_burn_percent,omitempty"`
	ActivePoolPercent7d      float64            `json:"active_pool_percent_7d"`
	Note                     string             `json:"note"`
	Warnings                 []ProviderWarning  `json:"warnings"`
	GeneratedAt              time.Time          `json:"generated_at"`
}

type CredentialListQuery struct {
	Page          int
	PageSize      int
	Search        string
	Provider      string
	Status        string
	ActivityRange string
	SortBy        string
}

type ProviderFacet struct {
	Provider string `json:"provider"`
	Count    int64  `json:"count"`
}

type ProviderCredentialRow struct {
	ID                       string     `json:"id"`
	SourceAuthID             string     `json:"runtime_id"`
	FileName                 string     `json:"credential_name"`
	SelectionKey             string     `json:"selection_key"`
	Provider                 string     `json:"provider"`
	Label                    string     `json:"label"`
	AccountKey               string     `json:"account_key"`
	AccountEmail             string     `json:"account_email"`
	Status                   string     `json:"status"`
	StatusMessage            string     `json:"status_message"`
	Disabled                 bool       `json:"disabled"`
	Unavailable              bool       `json:"unavailable"`
	RuntimeOnly              bool       `json:"runtime_only"`
	QuotaExceeded            bool       `json:"quota_exceeded"`
	LastRefreshAt            *time.Time `json:"last_refresh_at,omitempty"`
	LastSeenAt               *time.Time `json:"last_seen_at,omitempty"`
	UpdatedAt                time.Time  `json:"updated_at"`
	Requests24h              int64      `json:"requests_24h"`
	Requests7d               int64      `json:"requests_7d"`
	Failures24h              int64      `json:"failures_24h"`
	FailureRate24h           float64    `json:"failure_rate_24h"`
	TotalTokens24h           int64      `json:"total_tokens_24h"`
	TotalTokens7d            int64      `json:"total_tokens_7d"`
	HealthPercent            *float64   `json:"health_percent,omitempty"`
	ConservativeRiskDays     *float64   `json:"conservative_risk_days,omitempty"`
	AvgDailyQuotaBurnPercent *float64   `json:"avg_daily_quota_burn_percent,omitempty"`
	SnapshotMode             string     `json:"snapshot_mode"`
}

type CredentialListResult struct {
	Items          []ProviderCredentialRow `json:"items"`
	Page           int                     `json:"page"`
	PageSize       int                     `json:"page_size"`
	Total          int64                   `json:"total"`
	ProviderFacets []ProviderFacet         `json:"provider_facets,omitempty"`
}

type CredentialDetail struct {
	ProviderCredentialRow
	SecretVersion int              `json:"secret_version"`
	Metadata      map[string]any   `json:"metadata"`
	Dimensions    []QuotaDimension `json:"dimensions"`
}

type QuotaDimension struct {
	ID             string     `json:"id"`
	Label          string     `json:"label"`
	RemainingRatio *float64   `json:"remaining_ratio,omitempty"`
	ResetAt        *time.Time `json:"reset_at,omitempty"`
	WindowSeconds  *int       `json:"window_seconds,omitempty"`
	State          string     `json:"state"`
	ObservedAt     time.Time  `json:"observed_at"`
}

type QuotaRefreshPolicy struct {
	Provider              string `json:"provider"`
	Enabled               bool   `json:"enabled"`
	IntervalSeconds       int    `json:"interval_seconds"`
	TimeoutSeconds        int    `json:"timeout_seconds"`
	MaxParallelism        int    `json:"max_parallelism"`
	StaleAfterSeconds     int    `json:"stale_after_seconds"`
	FailureBackoffSeconds int    `json:"failure_backoff_seconds"`
}

type QuotaRefreshPoliciesResponse struct {
	Policies  []QuotaRefreshPolicy `json:"policies"`
	UpdatedAt time.Time            `json:"updated_at"`
	Source    string               `json:"source"`
}

type UsageEvent struct {
	EventKey        string    `json:"event_key"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	AuthID          string    `json:"runtime_id"`
	SelectionKey    string    `json:"selection_key"`
	Source          string    `json:"source"`
	RequestID       string    `json:"request_id"`
	RequestedAt     time.Time `json:"requested_at"`
	Failed          bool      `json:"failed"`
	InputTokens     int64     `json:"input_tokens"`
	OutputTokens    int64     `json:"output_tokens"`
	ReasoningTokens int64     `json:"reasoning_tokens"`
	CachedTokens    int64     `json:"cached_tokens"`
	TotalTokens     int64     `json:"total_tokens"`
}

type RequestTraceEvent struct {
	EventKey          string    `json:"event_key"`
	RequestID         string    `json:"request_id"`
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	AuthID            string    `json:"runtime_id"`
	SelectionKey      string    `json:"selection_key"`
	Source            string    `json:"source"`
	SourceDisplayName string    `json:"source_display_name"`
	SourceType        string    `json:"source_type"`
	CredentialID      string    `json:"credential_id,omitempty"`
	SourceAuthID      string    `json:"runtime_id,omitempty"`
	FileName          string    `json:"credential_name,omitempty"`
	Label             string    `json:"label,omitempty"`
	RequestedAt       time.Time `json:"requested_at"`
	Failed            bool      `json:"failed"`
	InputTokens       int64     `json:"input_tokens"`
	OutputTokens      int64     `json:"output_tokens"`
	ReasoningTokens   int64     `json:"reasoning_tokens"`
	CachedTokens      int64     `json:"cached_tokens"`
	TotalTokens       int64     `json:"total_tokens"`
}

type RequestTraceResult struct {
	RequestID string              `json:"request_id"`
	Items     []RequestTraceEvent `json:"items"`
}
