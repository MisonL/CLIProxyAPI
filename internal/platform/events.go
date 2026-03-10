package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

const (
	subjectUsageRecorded            = "cpa.events.usage.recorded"
	subjectCredentialChanged        = "cpa.events.credential.changed"
	subjectProjectionRebuildRequest = "cpa.events.projection.rebuild.requested"
	subjectQuotaObserved            = "cpa.events.quota.observed"
)

func BuildUsageEvent(record coreusage.Record) UsageEvent {
	requestedAt := record.RequestedAt.UTC()
	if requestedAt.IsZero() {
		requestedAt = time.Now().UTC()
	}
	event := UsageEvent{
		Provider:        record.Provider,
		Model:           record.Model,
		AuthID:          record.AuthID,
		SelectionKey:    record.SelectionKey,
		Source:          record.Source,
		RequestID:       strings.TrimSpace(record.RequestID),
		RequestedAt:     requestedAt,
		Failed:          record.Failed,
		InputTokens:     record.Detail.InputTokens,
		OutputTokens:    record.Detail.OutputTokens,
		ReasoningTokens: record.Detail.ReasoningTokens,
		CachedTokens:    record.Detail.CachedTokens,
		TotalTokens:     record.Detail.TotalTokens,
	}
	event.EventKey = buildUsageEventKey(event)
	return event
}

func buildUsageEventKey(event UsageEvent) string {
	hash := sha256.Sum256([]byte(fmt.Sprintf(
		"%s|%s|%s|%s|%s|%s|%d|%t|%d|%d|%d|%d|%d",
		event.Provider,
		event.Model,
		event.AuthID,
		event.SelectionKey,
		event.Source,
		event.RequestID,
		event.RequestedAt.UnixNano(),
		event.Failed,
		event.InputTokens,
		event.OutputTokens,
		event.ReasoningTokens,
		event.CachedTokens,
		event.TotalTokens,
	)))
	return hex.EncodeToString(hash[:])
}
