package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/redis/go-redis/v9"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	log "github.com/sirupsen/logrus"
)

type Runtime struct {
	cfg         Config
	store       *store
	redis       *redis.Client
	nats        *nats.Conn
	js          nats.JetStreamContext
	connectedAt time.Time
}

func NewRuntime(ctx context.Context, cfg Config) (*Runtime, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	dbStore, err := newStore(ctx, cfg)
	if err != nil {
		return nil, err
	}
	redisOptions, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		dbStore.close()
		return nil, fmt.Errorf("platform: parse redis url: %w", err)
	}
	redisClient := redis.NewClient(redisOptions)
	if err = redisClient.Ping(ctx).Err(); err != nil {
		dbStore.close()
		_ = redisClient.Close()
		return nil, fmt.Errorf("platform: ping redis: %w", err)
	}
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		dbStore.close()
		_ = redisClient.Close()
		return nil, fmt.Errorf("platform: connect NATS: %w", err)
	}
	js, err := nc.JetStream(nats.PublishAsyncMaxPending(256))
	if err != nil {
		dbStore.close()
		_ = redisClient.Close()
		nc.Close()
		return nil, fmt.Errorf("platform: create jetstream context: %w", err)
	}
	if _, err = js.AddStream(&nats.StreamConfig{
		Name:      cfg.NATSStream,
		Subjects:  []string{subjectUsageRecorded, subjectCredentialChanged, subjectProjectionRebuildRequest, subjectQuotaObserved},
		Retention: nats.LimitsPolicy,
		Storage:   nats.FileStorage,
		MaxAge:    7 * 24 * time.Hour,
	}); err != nil && err != nats.ErrStreamNameAlreadyInUse {
		dbStore.close()
		_ = redisClient.Close()
		nc.Close()
		return nil, fmt.Errorf("platform: ensure stream: %w", err)
	}
	return &Runtime{
		cfg:         cfg,
		store:       dbStore,
		redis:       redisClient,
		nats:        nc,
		js:          js,
		connectedAt: time.Now().UTC(),
	}, nil
}

func (r *Runtime) Close() error {
	if r == nil {
		return nil
	}
	if r.redis != nil {
		_ = r.redis.Close()
	}
	if r.nats != nil {
		r.nats.Close()
	}
	if r.store != nil {
		r.store.close()
	}
	return nil
}

func (r *Runtime) Config() Config {
	if r == nil {
		return Config{}
	}
	return r.cfg
}

func (r *Runtime) Status() RuntimeStatus {
	if r == nil {
		return RuntimeStatus{}
	}
	return RuntimeStatus{
		Enabled:        r.cfg.Enabled,
		Role:           r.cfg.Role,
		DatabaseSchema: r.cfg.DatabaseSchema,
		TenantSlug:     r.cfg.TenantSlug,
		WorkspaceSlug:  r.cfg.WorkspaceSlug,
		NATSStream:     r.cfg.NATSStream,
		CachePrefix:    r.cfg.CachePrefix,
		ConnectedAt:    r.connectedAt,
	}
}

func (r *Runtime) HealthCheck(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("platform runtime unavailable")
	}
	if r.store == nil || r.store.pool == nil {
		return fmt.Errorf("platform database unavailable")
	}
	if err := r.store.pool.Ping(ctx); err != nil {
		return fmt.Errorf("platform database ping failed: %w", err)
	}
	if r.redis == nil {
		return fmt.Errorf("platform redis unavailable")
	}
	if err := r.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("platform redis ping failed: %w", err)
	}
	if r.nats == nil || !r.nats.IsConnected() {
		return fmt.Errorf("platform nats unavailable")
	}
	if r.js == nil {
		return fmt.Errorf("platform jetstream unavailable")
	}
	if _, err := r.js.AccountInfo(); err != nil {
		return fmt.Errorf("platform jetstream check failed: %w", err)
	}
	return nil
}

func (r *Runtime) SyncAuthSnapshot(ctx context.Context, credentials []*coreauth.Auth) error {
	if r == nil || r.store == nil {
		return nil
	}
	for _, auth := range credentials {
		if auth == nil {
			continue
		}
		if err := r.store.upsertAuth(ctx, auth); err != nil {
			return err
		}
		if err := r.publishCredentialChanged(ctx, auth.ID, normalizeProviderOrUnknown(auth.Provider)); err != nil {
			log.WithError(err).Warn("platform: publish credential change failed")
		}
		r.invalidateProviderCache(ctx, auth.Provider)
	}
	return nil
}

func (r *Runtime) SyncAuth(ctx context.Context, auth *coreauth.Auth) error {
	if r == nil || r.store == nil || auth == nil {
		return nil
	}
	if err := r.store.upsertAuth(ctx, auth); err != nil {
		return err
	}
	r.invalidateProviderCache(ctx, auth.Provider)
	return r.publishCredentialChanged(ctx, auth.ID, normalizeProviderOrUnknown(auth.Provider))
}

func (r *Runtime) DeleteAuth(ctx context.Context, authID string) error {
	if r == nil || r.store == nil {
		return nil
	}
	if err := r.store.markAuthDeleted(ctx, authID); err != nil {
		return err
	}
	r.invalidateAllOverviewCaches(ctx)
	return nil
}

func (r *Runtime) ProviderOverview(ctx context.Context, provider string) (ProviderOverview, error) {
	provider = normalizeProvider(provider)
	if cached, ok := r.readOverviewCache(ctx, provider); ok {
		return cached, nil
	}
	overview, err := r.store.providerOverview(ctx, provider)
	if err != nil {
		return ProviderOverview{}, err
	}
	r.writeOverviewCache(ctx, provider, overview)
	return overview, nil
}

func (r *Runtime) GetHistogramBucketItems(ctx context.Context, provider string, datasetID string, bucketIndex int, page int, pageSize int) (HistogramBucketItemsResponse, error) {
	if r == nil || r.store == nil {
		return HistogramBucketItemsResponse{}, fmt.Errorf("platform runtime unavailable")
	}
	return r.store.listHistogramBucketItems(ctx, provider, datasetID, bucketIndex, page, pageSize)
}

func (r *Runtime) ListProviderCredentials(ctx context.Context, provider string, query CredentialListQuery) (CredentialListResult, error) {
	return r.store.listProviderCredentials(ctx, provider, query)
}

func (r *Runtime) ListCredentials(ctx context.Context, query CredentialListQuery) (CredentialListResult, error) {
	if r == nil || r.store == nil {
		return CredentialListResult{}, nil
	}
	return r.store.listCredentials(ctx, query)
}

func (r *Runtime) GetCredentialDetail(ctx context.Context, credentialID string) (CredentialDetail, error) {
	if r == nil || r.store == nil {
		return CredentialDetail{}, fmt.Errorf("platform runtime unavailable")
	}
	return r.store.getCredentialDetail(ctx, credentialID)
}

func (r *Runtime) GetTraceByRequestID(ctx context.Context, requestID string) (RequestTraceResult, error) {
	if r == nil || r.store == nil {
		return RequestTraceResult{}, fmt.Errorf("platform runtime unavailable")
	}
	return r.store.getTraceByRequestID(ctx, requestID)
}

func (r *Runtime) DownloadCredentialContent(ctx context.Context, credentialID string) (string, string, []byte, error) {
	if r == nil || r.store == nil {
		return "", "", nil, fmt.Errorf("platform runtime unavailable")
	}
	return r.store.downloadCredentialContent(ctx, credentialID)
}

func (r *Runtime) GetQuotaRefreshPolicies(ctx context.Context) (QuotaRefreshPoliciesResponse, error) {
	if r == nil || r.store == nil {
		return QuotaRefreshPoliciesResponse{}, fmt.Errorf("platform runtime unavailable")
	}
	return r.store.getQuotaRefreshPolicies(ctx)
}

func (r *Runtime) SetQuotaRefreshPolicies(ctx context.Context, policies []QuotaRefreshPolicy) (QuotaRefreshPoliciesResponse, error) {
	if r == nil || r.store == nil {
		return QuotaRefreshPoliciesResponse{}, fmt.Errorf("platform runtime unavailable")
	}
	if err := r.store.setQuotaRefreshPolicies(ctx, policies); err != nil {
		return QuotaRefreshPoliciesResponse{}, err
	}
	return r.store.getQuotaRefreshPolicies(ctx)
}

func (r *Runtime) RefreshProvider(ctx context.Context, provider string) error {
	r.invalidateProviderCache(ctx, provider)
	return r.publishProjectionRebuild(ctx, normalizeProvider(provider))
}

func (r *Runtime) PublishUsageEvent(ctx context.Context, event UsageEvent) error {
	if r == nil || r.js == nil {
		return nil
	}
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = r.js.Publish(subjectUsageRecorded, body)
	return err
}

func (r *Runtime) publishCredentialChanged(ctx context.Context, authID, provider string) error {
	if r == nil || r.js == nil {
		return nil
	}
	body, err := json.Marshal(map[string]string{
		"auth_id":  authID,
		"provider": provider,
	})
	if err != nil {
		return err
	}
	_, err = r.js.Publish(subjectCredentialChanged, body)
	return err
}

func (r *Runtime) publishProjectionRebuild(ctx context.Context, provider string) error {
	if r == nil || r.js == nil {
		return nil
	}
	body, err := json.Marshal(map[string]string{
		"provider": provider,
	})
	if err != nil {
		return err
	}
	_, err = r.js.Publish(subjectProjectionRebuildRequest, body)
	return err
}

func (r *Runtime) BackfillCurrentUsage(ctx context.Context) error {
	if r == nil || r.store == nil {
		return nil
	}
	snapshot := usage.GetRequestStatistics().Snapshot()
	events := make(map[string]map[string][]UsageEvent)
	for providerName, apiSnapshot := range snapshot.APIs {
		for modelName, modelSnapshot := range apiSnapshot.Models {
			for _, detail := range modelSnapshot.Details {
				provider := providerName
				if resolvedProvider, errLookup := r.store.lookupProviderBySelectionKey(ctx, detail.SelectionKey); errLookup == nil && resolvedProvider != "" {
					provider = resolvedProvider
				}
				provider = normalizeProvider(provider)
				events = buildSnapshotFromUsageDetails(events, UsageEvent{
					EventKey: buildUsageEventKey(UsageEvent{
						Provider:        provider,
						Model:           modelName,
						SelectionKey:    detail.SelectionKey,
						Source:          detail.Source,
						RequestedAt:     detail.Timestamp.UTC(),
						Failed:          detail.Failed,
						InputTokens:     detail.Tokens.InputTokens,
						OutputTokens:    detail.Tokens.OutputTokens,
						ReasoningTokens: detail.Tokens.ReasoningTokens,
						CachedTokens:    detail.Tokens.CachedTokens,
						TotalTokens:     detail.Tokens.TotalTokens,
					}),
					Provider:        provider,
					Model:           modelName,
					SelectionKey:    detail.SelectionKey,
					Source:          detail.Source,
					RequestedAt:     detail.Timestamp.UTC(),
					Failed:          detail.Failed,
					InputTokens:     detail.Tokens.InputTokens,
					OutputTokens:    detail.Tokens.OutputTokens,
					ReasoningTokens: detail.Tokens.ReasoningTokens,
					CachedTokens:    detail.Tokens.CachedTokens,
					TotalTokens:     detail.Tokens.TotalTokens,
				})
			}
		}
	}
	_, err := r.store.backfillUsageSnapshot(ctx, events)
	return err
}

func (r *Runtime) readOverviewCache(ctx context.Context, provider string) (ProviderOverview, bool) {
	if r == nil || r.redis == nil || r.cfg.CacheTTL <= 0 {
		return ProviderOverview{}, false
	}
	payload, err := r.redis.Get(ctx, r.overviewCacheKey(provider)).Result()
	if err != nil || strings.TrimSpace(payload) == "" {
		return ProviderOverview{}, false
	}
	var overview ProviderOverview
	if err = json.Unmarshal([]byte(payload), &overview); err != nil {
		return ProviderOverview{}, false
	}
	return overview, true
}

func (r *Runtime) writeOverviewCache(ctx context.Context, provider string, overview ProviderOverview) {
	if r == nil || r.redis == nil || r.cfg.CacheTTL <= 0 {
		return
	}
	payload, err := json.Marshal(overview)
	if err != nil {
		return
	}
	_ = r.redis.Set(ctx, r.overviewCacheKey(provider), payload, r.cfg.CacheTTL).Err()
}

func (r *Runtime) invalidateProviderCache(ctx context.Context, provider string) {
	if r == nil || r.redis == nil {
		return
	}
	_ = r.redis.Del(ctx, r.overviewCacheKey(normalizeProvider(provider))).Err()
}

func (r *Runtime) invalidateAllOverviewCaches(ctx context.Context) {
	if r == nil || r.redis == nil {
		return
	}
	iter := r.redis.Scan(ctx, 0, r.cfg.CachePrefix+":overview:*", 50).Iterator()
	for iter.Next(ctx) {
		_ = r.redis.Del(ctx, iter.Val()).Err()
	}
}

func (r *Runtime) overviewCacheKey(provider string) string {
	return fmt.Sprintf("%s:overview:%s:%s", r.cfg.CachePrefix, r.cfg.WorkspaceSlug, normalizeProvider(provider))
}
