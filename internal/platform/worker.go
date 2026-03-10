package platform

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/nats-io/nats.go"
	log "github.com/sirupsen/logrus"
)

func (r *Runtime) RunWorker(ctx context.Context) error {
	if r == nil || r.js == nil || r.store == nil {
		return nil
	}
	sub, err := r.js.PullSubscribe(subjectUsageRecorded, "cpa-platform-worker-usage", nats.BindStream(r.cfg.NATSStream))
	if err != nil {
		return err
	}
	defer func() { _ = sub.Unsubscribe() }()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msgs, errFetch := sub.Fetch(32, nats.MaxWait(2*time.Second))
		if errFetch != nil {
			if errors.Is(errFetch, nats.ErrTimeout) {
				continue
			}
			return errFetch
		}
		for _, msg := range msgs {
			if err = r.handleWorkerMessage(ctx, msg); err != nil {
				log.WithError(err).Warn("platform: worker message failed")
				_ = msg.Nak()
				continue
			}
			_ = msg.Ack()
		}
	}
}

func (r *Runtime) handleWorkerMessage(ctx context.Context, msg *nats.Msg) error {
	switch msg.Subject {
	case subjectUsageRecorded:
		var event UsageEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			return err
		}
		if err := r.store.ingestUsageEvent(ctx, event); err != nil {
			return err
		}
		r.invalidateProviderCache(ctx, event.Provider)
		return nil
	case subjectProjectionRebuildRequest, subjectCredentialChanged:
		return nil
	default:
		return nil
	}
}
