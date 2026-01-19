package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/auth/perplexity"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

// DoPerplexityCookieAuth performs the Perplexity cookie-based authentication.
func DoPerplexityCookieAuth(cfg *config.Config, options *LoginOptions) {
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

	// Show extraction instructions
	fmt.Println("\n=== Perplexity Cookie Authentication ===")
	fmt.Println("\nTo get your session cookie:")
	fmt.Println("1. Log into perplexity.ai in your browser")
	fmt.Println("2. Open DevTools (F12) → Application tab → Cookies → perplexity.ai")
	fmt.Println("3. Find '__Secure-next-auth.session-token' and copy its value")
	fmt.Println("\nAlternatively, run this in browser console:")
	fmt.Println("  copy(document.cookie)")
	fmt.Println()

	// Prompt user for cookie
	cookie, err := promptForPerplexityCookie(promptFn)
	if err != nil {
		fmt.Printf("Failed to get cookie: %v\n", err)
		return
	}

	// Check for duplicate session token before saving
	if existingFile, err := perplexity.CheckDuplicateSessionToken(cfg.AuthDir, cookie); err != nil {
		fmt.Printf("Failed to check duplicate: %v\n", err)
		return
	} else if existingFile != "" {
		fmt.Printf("Duplicate session found, authentication already exists: %s\n", filepath.Base(existingFile))
		return
	}

	// Create token data - we use "user" as email since we can't easily extract it from the session
	tokenData := &perplexity.PerplexityTokenData{
		SessionToken: cookie,
		Email:        "user", // Could potentially extract from JWT payload if needed
	}

	// Create token storage
	auth := perplexity.NewPerplexityAuth()
	tokenStorage := auth.CreateTokenStorage(tokenData)

	// Get auth file path
	authFilePath := getPerplexityAuthFilePath(cfg, tokenData.Email)

	// Save token to file
	if err := tokenStorage.SaveTokenToFile(authFilePath); err != nil {
		fmt.Printf("Failed to save authentication: %v\n", err)
		return
	}

	fmt.Println("\n✓ Authentication saved successfully!")
	fmt.Printf("  File: %s\n", authFilePath)
	fmt.Println("\nAvailable models via Perplexity Pro:")
	fmt.Println("  • claude-4.5-sonnet (claude45sonnet)")
	fmt.Println("  • gpt-5.2 (gpt52)")
	fmt.Println("  • claude-4.5-sonnet-thinking (claude45sonnetthinking)")
	fmt.Println("  • gpt-5.2-thinking (gpt52_thinking)")
	fmt.Println("  • gemini-3.0-pro (gemini30pro)")
	fmt.Println("  • kimi-k2-thinking (kimik2thinking)")
	fmt.Println("  • grok-4.1 (grok41nonreasoning)")
	fmt.Println("  • grok-4.1-reasoning (grok41reasoning)")
}

// promptForPerplexityCookie prompts the user to enter their Perplexity session cookie
func promptForPerplexityCookie(promptFn func(string) (string, error)) (string, error) {
	line, err := promptFn("Enter session token (or full cookie string): ")
	if err != nil {
		return "", fmt.Errorf("failed to read cookie: %w", err)
	}

	cookie, err := perplexity.NormalizeCookie(line)
	if err != nil {
		return "", err
	}

	return cookie, nil
}

// getPerplexityAuthFilePath returns the auth file path for perplexity
func getPerplexityAuthFilePath(cfg *config.Config, email string) string {
	fileName := perplexity.SanitizeFileName(email)
	return fmt.Sprintf("%s/perplexity-%s-%d.json", cfg.AuthDir, fileName, time.Now().Unix())
}
