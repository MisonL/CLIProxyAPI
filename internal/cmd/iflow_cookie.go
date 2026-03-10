package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/iflow"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

// DoIFlowCookieAuth performs the iFlow cookie-based authentication.
func DoIFlowCookieAuth(cfg *config.Config, options *LoginOptions) {
	if options == nil {
		options = &LoginOptions{}
	}

	promptFn := options.Prompt
	if promptFn == nil {
		reader := bufio.NewReader(os.Stdin)
		promptFn = func(prompt string) (string, error) {
			fmt.Print(prompt)
			value, err := reader.ReadString('\n')
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(value), nil
		}
	}

	// Prompt user for cookie
	cookie, err := promptForCookie(promptFn)
	if err != nil {
		fmt.Printf("Failed to get cookie: %v\n", err)
		return
	}

	ctx := context.Background()

	handle, errStore := newCommandAuthStore(cfg)
	if errStore != nil {
		fmt.Printf("Failed to initialize auth store: %v\n", errStore)
		return
	}
	defer func() {
		if handle.close != nil {
			_ = handle.close()
		}
	}()

	// Check for duplicate BXAuth before authentication
	bxAuth := iflow.ExtractBXAuth(cookie)
	if existingRef, err := findExistingIFlowCredentialByBXAuth(ctx, handle.store, bxAuth); err != nil {
		fmt.Printf("Failed to check duplicate: %v\n", err)
		return
	} else if existingRef != "" {
		fmt.Printf("Duplicate BXAuth found, credential already exists: %s\n", existingRef)
		return
	}

	// Authenticate with cookie
	auth := iflow.NewIFlowAuth(cfg)
	tokenData, err := auth.AuthenticateWithCookie(ctx, cookie)
	if err != nil {
		fmt.Printf("iFlow cookie authentication failed: %v\n", err)
		return
	}

	// Create token storage
	tokenStorage := auth.CreateCookieTokenStorage(tokenData)

	identifier := strings.TrimSpace(tokenData.Email)
	if identifier == "" {
		identifier = fmt.Sprintf("%d", time.Now().UnixMilli())
	}
	tokenStorage.Email = identifier
	fileName := getIFlowAuthFileName(identifier)
	record := &coreauth.Auth{
		ID:       fileName,
		Provider: "iflow",
		FileName: fileName,
		Storage:  tokenStorage,
		Metadata: map[string]any{
			"email":        identifier,
			"api_key":      tokenStorage.APIKey,
			"expired":      tokenStorage.Expire,
			"cookie":       tokenStorage.Cookie,
			"type":         tokenStorage.Type,
			"last_refresh": tokenStorage.LastRefresh,
		},
		Attributes: map[string]string{
			"api_key": tokenStorage.APIKey,
		},
	}

	credentialRef, err := handle.store.Save(ctx, record)
	if err != nil {
		fmt.Printf("Failed to save authentication: %v\n", err)
		return
	}

	fmt.Printf("Authentication successful! API key: %s\n", tokenData.APIKey)
	fmt.Printf("Expires at: %s\n", tokenData.Expire)
	fmt.Printf("Credential saved as %s\n", credentialRef)
}

// promptForCookie prompts the user to enter their iFlow cookie
func promptForCookie(promptFn func(string) (string, error)) (string, error) {
	line, err := promptFn("Enter iFlow Cookie (from browser cookies): ")
	if err != nil {
		return "", fmt.Errorf("failed to read cookie: %w", err)
	}

	cookie, err := iflow.NormalizeCookie(line)
	if err != nil {
		return "", err
	}

	return cookie, nil
}

func getIFlowAuthFileName(email string) string {
	fileName := iflow.SanitizeIFlowFileName(email)
	return fmt.Sprintf("iflow-%s-%d.json", fileName, time.Now().Unix())
}

func findExistingIFlowCredentialByBXAuth(ctx context.Context, store coreauth.Store, bxAuth string) (string, error) {
	if strings.TrimSpace(bxAuth) == "" || store == nil {
		return "", nil
	}

	items, err := store.List(ctx)
	if err != nil {
		return "", err
	}

	for _, item := range items {
		if item == nil || !strings.EqualFold(item.Provider, "iflow") {
			continue
		}
		if existing := iflow.ExtractBXAuth(strings.TrimSpace(metadataString(item.Metadata, "cookie"))); existing == bxAuth {
			return firstNonEmpty(item.ID, item.FileName), nil
		}
	}

	return "", nil
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
