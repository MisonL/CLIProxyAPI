package cmd

import (
	"context"
	"fmt"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v6/sdk/auth"
	log "github.com/sirupsen/logrus"
)

// DoKimiLogin triggers the OAuth device flow for Kimi (Moonshot AI) and saves tokens.
// It initiates the device flow authentication, displays the verification URL for the user,
// and waits for authorization before saving the tokens.
//
// Parameters:
//   - cfg: The application configuration containing proxy and credentials directory settings
//   - options: Login options including browser behavior settings
func DoKimiLogin(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}

	manager, closeStore, errStore := newAuthManager(cfg)
	if errStore != nil {
		log.Errorf("Kimi authentication setup failed: %v", errStore)
		return
	}
	defer closeStore()
	authOpts := &sdkAuth.LoginOptions{
		NoBrowser: options.NoBrowser,
		Metadata:  map[string]string{},
		Prompt:    options.Prompt,
	}

	record, savedPath, err := manager.Login(context.Background(), "kimi", cfg, authOpts)
	if err != nil {
		log.Errorf("Kimi authentication failed: %v", err)
		return
	}

	if savedPath != "" {
		fmt.Printf("Credential saved as %s\n", savedPath)
	}
	if record != nil && record.Label != "" {
		fmt.Printf("Authenticated as %s\n", record.Label)
	}
	fmt.Println("Kimi authentication successful!")
}
