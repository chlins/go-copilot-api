package config

import (
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// Config holds application configuration loaded from environment variables or defaults.
type Config struct {
	ServerAddr         string
	Debug              bool
	CopilotOAuthToken  string
	CopilotToken       string       // API access token for authentication
	ServerPort         string       // Port to listen on (default: 9191)
	CORSAllowedOrigins string       // Comma-separated list of allowed CORS origins (default: *)
	DefaultModel       string       // Default model to use if not specified in request
	HTTPProxy          string       // HTTP proxy URL (read from HTTP_PROXY, HTTPS_PROXY, or ALL_PROXY)
	HTTPClient         *http.Client // Shared HTTP client with proxy support
}

// Load reads configuration from environment variables, falling back to sensible defaults.
// It will attempt to load the Copilot OAuth token from the environment variable COPILOT_OAUTH_TOKEN,
// or, if not set, from the GitHub Copilot apps.json file in the user's config directory.
func Load() (*Config, error) {
	// Load HTTP proxy from environment (check multiple common env vars)
	httpProxy := getEnv("HTTPS_PROXY", "")
	if httpProxy == "" {
		httpProxy = getEnv("https_proxy", "")
	}
	if httpProxy == "" {
		httpProxy = getEnv("HTTP_PROXY", "")
	}
	if httpProxy == "" {
		httpProxy = getEnv("http_proxy", "")
	}
	if httpProxy == "" {
		httpProxy = getEnv("ALL_PROXY", "")
	}
	if httpProxy == "" {
		httpProxy = getEnv("all_proxy", "")
	}

	cfg := &Config{
		ServerAddr:         getEnv("SERVER_ADDR", ":8080"),
		Debug:              getEnvBool("DEBUG", false),
		CopilotToken:       getEnv("COPILOT_TOKEN", randomToken()),
		ServerPort:         getEnv("COPILOT_SERVER_PORT", "9191"),
		CORSAllowedOrigins: getEnv("CORS_ALLOWED_ORIGINS", "*"),
		DefaultModel:       getEnv("DEFAULT_MODEL", ""),
		HTTPProxy:          httpProxy,
	}

	// Create HTTP client with proxy support
	httpClient, err := createHTTPClient(httpProxy)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client with proxy: %w", err)
	}
	cfg.HTTPClient = httpClient

	if httpProxy != "" {
		fmt.Printf("Using HTTP proxy: %s\n", httpProxy)
	}

	// Try to get Copilot OAuth token from env first
	token := getEnv("COPILOT_OAUTH_TOKEN", "")
	if token == "" {
		// Try to auto-detect from apps.json
		token = findCopilotToken()
	}
	cfg.CopilotOAuthToken = token

	if cfg.CopilotOAuthToken == "" {
		fmt.Fprintln(os.Stderr, "Warning: Copilot OAuth token not found in environment or apps.json")
	}

	return cfg, nil
}

// getEnv returns the value of the environment variable if set, otherwise returns the default.
func getEnv(key, def string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return def
}

// getEnvBool returns the boolean value of the environment variable if set, otherwise returns the default.
func getEnvBool(key string, def bool) bool {
	val, ok := os.LookupEnv(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(val)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid boolean for %s: %v, using default %v\n", key, err, def)
		return def
	}
	return b
}

// randomToken generates a random fallback token if COPILOT_TOKEN is not set.
func randomToken() string {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	if err != nil {
		// fallback to a static string if random fails
		return "default-token"
	}
	return fmt.Sprintf("%x", b)
}

// findCopilotToken attempts to locate and parse the Copilot OAuth token from the user's config directory.
// Checks platform-specific locations for apps.json and returns the first oauth_token found.
func findCopilotToken() string {
	var configPath string
	if runtime.GOOS == "windows" {
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData != "" {
			configPath = filepath.Join(localAppData, "github-copilot", "apps.json")
		}
	} else {
		home, err := os.UserHomeDir()
		if err == nil {
			configPath = filepath.Join(home, ".config", "github-copilot", "apps.json")
		}
	}
	if configPath == "" {
		return ""
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var apps map[string]struct {
		User        string `json:"user"`
		OAuthToken  string `json:"oauth_token"`
		GitHubAppId string `json:"githubAppId"`
	}
	if err := json.Unmarshal(data, &apps); err != nil {
		return ""
	}
	for _, v := range apps {
		if v.OAuthToken != "" {
			return v.OAuthToken
		}
	}
	return ""
}

// createHTTPClient creates an HTTP client with optional proxy support.
func createHTTPClient(proxyURL string) (*http.Client, error) {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
		},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return nil, fmt.Errorf("invalid proxy URL %q: %w", proxyURL, err)
		}
		transport.Proxy = http.ProxyURL(parsed)
	}

	return &http.Client{
		Transport: transport,
		Timeout:   30 * time.Second,
	}, nil
}

// GetHTTPClient returns the configured HTTP client with proxy support.
// This should be used for all outgoing HTTP requests.
func (c *Config) GetHTTPClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	// Fallback to default client if not configured
	return http.DefaultClient
}
