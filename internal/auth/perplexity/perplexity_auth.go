package perplexity

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// SessionTokenCookieName is the NextAuth session cookie name used by Perplexity.
	SessionTokenCookieName = "__Secure-next-auth.session-token"
)

// PerplexityAuth handles Perplexity authentication operations.
type PerplexityAuth struct{}

// NewPerplexityAuth creates a new PerplexityAuth instance.
func NewPerplexityAuth() *PerplexityAuth {
	return &PerplexityAuth{}
}

// PerplexityTokenData captures processed token details.
type PerplexityTokenData struct {
	SessionToken string
	Email        string
}

// NormalizeCookie normalizes raw cookie strings for Perplexity authentication.
// Accepts either:
// - Full cookie string with multiple cookies
// - Just the session token value
func NormalizeCookie(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", fmt.Errorf("cookie cannot be empty")
	}

	// Check if it's already a raw session token (JWT format starts with eyJ)
	if strings.HasPrefix(trimmed, "eyJ") {
		return trimmed, nil
	}

	// Extract session token from cookie string
	sessionToken := ExtractSessionToken(trimmed)
	if sessionToken == "" {
		return "", fmt.Errorf("cookie missing %s field", SessionTokenCookieName)
	}

	return sessionToken, nil
}

// ExtractSessionToken extracts the NextAuth session token from a cookie string.
func ExtractSessionToken(cookie string) string {
	parts := strings.Split(cookie, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, SessionTokenCookieName+"=") {
			return strings.TrimPrefix(part, SessionTokenCookieName+"=")
		}
	}
	return ""
}

// SanitizeFileName normalizes user identifiers for safe filename usage.
func SanitizeFileName(raw string) string {
	if raw == "" {
		return "user"
	}
	cleanEmail := strings.ReplaceAll(raw, "*", "x")
	var result strings.Builder
	for _, r := range cleanEmail {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '@' || r == '.' || r == '-' {
			result.WriteRune(r)
		}
	}
	sanitized := strings.TrimSpace(result.String())
	if sanitized == "" {
		return "user"
	}
	return sanitized
}

// CheckDuplicateSessionToken checks if the given session token already exists in any perplexity auth file.
// Returns the path of the existing file if found, empty string otherwise.
func CheckDuplicateSessionToken(authDir, sessionToken string) (string, error) {
	if sessionToken == "" {
		return "", nil
	}

	entries, err := os.ReadDir(authDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read auth dir failed: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, "perplexity-") || !strings.HasSuffix(name, ".json") {
			continue
		}

		filePath := filepath.Join(authDir, name)
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var tokenData struct {
			SessionToken string `json:"session_token"`
		}
		if err := json.Unmarshal(data, &tokenData); err != nil {
			continue
		}

		if tokenData.SessionToken != "" && tokenData.SessionToken == sessionToken {
			return filePath, nil
		}
	}

	return "", nil
}

// CreateTokenStorage converts token data into persistence storage.
func (pa *PerplexityAuth) CreateTokenStorage(data *PerplexityTokenData) *PerplexityTokenStorage {
	if data == nil {
		return nil
	}
	return &PerplexityTokenStorage{
		SessionToken: data.SessionToken,
		Email:        data.Email,
		LastRefresh:  time.Now().Format(time.RFC3339),
		Type:         "perplexity",
	}
}

// GetCookieExtractionScript returns the JavaScript snippet for extracting cookies from browser.
func GetCookieExtractionScript() string {
	return `// Paste this in browser console on perplexity.ai while logged in:
// Method 1: If cookies are accessible via JavaScript
copy(document.cookie.split(';').filter(c => c.includes('next-auth')).map(c => c.trim()).join('; '));
console.log('Cookies copied to clipboard!');

// Method 2: If the above doesn't work (HttpOnly cookies)
// 1. Open DevTools (F12) → Application tab → Cookies → perplexity.ai
// 2. Find "__Secure-next-auth.session-token" and copy its value`
}
