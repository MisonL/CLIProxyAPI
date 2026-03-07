package usage

import (
	"context"
	"sync"
	"testing"
)

type recordingPlugin struct {
	mu      sync.Mutex
	records []Record
}

func (p *recordingPlugin) HandleUsage(ctx context.Context, record Record) {
	_ = ctx
	p.mu.Lock()
	p.records = append(p.records, record)
	p.mu.Unlock()
}

func TestManagerStopDrainsQueuedRecords(t *testing.T) {
	manager := NewManager(1)
	plugin := &recordingPlugin{}
	manager.Register(plugin)

	manager.Publish(context.Background(), Record{Provider: "codex", Model: "gpt-5"})
	manager.Stop()

	plugin.mu.Lock()
	defer plugin.mu.Unlock()
	if len(plugin.records) != 1 {
		t.Fatalf("records = %d, want 1", len(plugin.records))
	}
}
