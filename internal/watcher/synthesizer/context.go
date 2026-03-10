package synthesizer

import (
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// SynthesisContext provides the context needed for auth synthesis.
type SynthesisContext struct {
	// Config is the current configuration
	Config *config.Config
	// CredentialsDir is the directory containing credential files
	CredentialsDir string
	// Now is the current time for timestamps
	Now time.Time
	// IDGenerator generates stable IDs for auth entries
	IDGenerator *StableIDGenerator
}
