package platform

import (
	"context"
	"sync"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

var registerPlatformPluginOnce sync.Once

type usagePlugin struct {
	runtime *Runtime
}

func RegisterUsagePlugin(runtime *Runtime) {
	if runtime == nil {
		return
	}
	registerPlatformPluginOnce.Do(func() {
		coreusage.RegisterPlugin(&usagePlugin{runtime: runtime})
	})
}

func (p *usagePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	if p == nil || p.runtime == nil {
		return
	}
	_ = p.runtime.PublishUsageEvent(ctx, BuildUsageEvent(record))
}
