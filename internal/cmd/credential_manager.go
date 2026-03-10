package cmd

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/platform"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v6/sdk/auth"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type authStoreHandle struct {
	store coreauth.Store
	close func() error
}

func newCommandAuthStore(cfg *config.Config) (*authStoreHandle, error) {
	if cfg == nil {
		cfg = &config.Config{}
	}

	platformCfg := platform.LoadConfigFromEnv()
	if !platformCfg.Enabled {
		return nil, fmt.Errorf("platform mode is required for credential management")
	}
	runtime, err := platform.NewRuntime(context.Background(), platformCfg)
	if err != nil {
		return nil, err
	}
	return &authStoreHandle{
		store: runtime,
		close: runtime.Close,
	}, nil
}

// newAuthManager creates a new authentication manager instance with all supported
// authenticators and a store chosen from current runtime mode.
func newAuthManager(cfg *config.Config) (*sdkAuth.Manager, func(), error) {
	handle, err := newCommandAuthStore(cfg)
	if err != nil {
		return nil, nil, err
	}

	manager := sdkAuth.NewManager(handle.store,
		sdkAuth.NewGeminiAuthenticator(),
		sdkAuth.NewCodexAuthenticator(),
		sdkAuth.NewClaudeAuthenticator(),
		sdkAuth.NewQwenAuthenticator(),
		sdkAuth.NewIFlowAuthenticator(),
		sdkAuth.NewAntigravityAuthenticator(),
		sdkAuth.NewKimiAuthenticator(),
	)

	return manager, func() {
		if handle.close != nil {
			_ = handle.close()
		}
	}, nil
}
