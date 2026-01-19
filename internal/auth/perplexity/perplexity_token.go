package perplexity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
)

// PerplexityTokenStorage persists Perplexity session credentials.
type PerplexityTokenStorage struct {
	SessionToken string `json:"session_token"`
	Email        string `json:"email"`
	LastRefresh  string `json:"last_refresh"`
	Type         string `json:"type"`
}

// SaveTokenToFile serialises the token storage to disk.
func (ts *PerplexityTokenStorage) SaveTokenToFile(authFilePath string) error {
	misc.LogSavingCredentials(authFilePath)
	ts.Type = "perplexity"
	if err := os.MkdirAll(filepath.Dir(authFilePath), 0o700); err != nil {
		return fmt.Errorf("perplexity token: create directory failed: %w", err)
	}

	f, err := os.Create(authFilePath)
	if err != nil {
		return fmt.Errorf("perplexity token: create file failed: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err = json.NewEncoder(f).Encode(ts); err != nil {
		return fmt.Errorf("perplexity token: encode token failed: %w", err)
	}
	return nil
}
