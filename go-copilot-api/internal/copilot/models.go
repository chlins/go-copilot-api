package copilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// ModelsCache holds the cached models list and manages refresh.
type ModelsCache struct {
	mu           sync.RWMutex
	modelsJSON   []byte
	lastFetch    time.Time
	ttl          time.Duration
	tokenManager *TokenManager
	httpClient   *http.Client
}

// NewModelsCache creates a new ModelsCache and fetches models on startup.
// tokenManager is used to get the Copilot API token for authentication.
// httpClient is optional - if nil, http.DefaultClient will be used.
func NewModelsCache(ctx context.Context, tokenManager *TokenManager, ttl time.Duration, httpClient *http.Client) (*ModelsCache, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	cache := &ModelsCache{
		ttl:          ttl,
		tokenManager: tokenManager,
		httpClient:   httpClient,
	}
	if err := cache.refresh(ctx); err != nil {
		return nil, err
	}
	return cache, nil
}

// GetModels returns the cached models JSON. If expired, it refreshes in the background.
func (c *ModelsCache) GetModels(ctx context.Context) ([]byte, error) {
	c.mu.RLock()
	models := c.modelsJSON
	expired := time.Since(c.lastFetch) > c.ttl
	c.mu.RUnlock()

	if !expired && len(models) > 0 {
		return models, nil
	}

	// Refresh in background if expired, but return stale data if available
	go c.refresh(context.Background())
	if len(models) > 0 {
		return models, nil
	}
	return nil, errors.New("models not available")
}

// refresh fetches the models list from the GitHub Copilot API.
func (c *ModelsCache) refresh(ctx context.Context) error {
	// Get fresh token from TokenManager
	token, err := c.tokenManager.GetToken(ctx)
	if err != nil {
		return fmt.Errorf("failed to get Copilot token: %w", err)
	}

	const modelsURL = "https://api.githubcopilot.com/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Editor-Version", "vscode/1.95.0")
	req.Header.Set("Copilot-Integration-Id", "vscode-chat")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("models API error: %s - %s", resp.Status, string(body))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// Validate JSON (Copilot API returns an object, not an array)
	var js interface{}
	if err := json.Unmarshal(data, &js); err != nil {
		return fmt.Errorf("invalid models JSON: %w", err)
	}

	c.mu.Lock()
	c.modelsJSON = data
	c.lastFetch = time.Now()
	c.mu.Unlock()
	return nil
}

// SaveToFile writes the cached models JSON to a file (optional).
func (c *ModelsCache) SaveToFile(path string) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if len(c.modelsJSON) == 0 {
		return errors.New("no models to save")
	}
	return os.WriteFile(path, c.modelsJSON, 0644)
}

// LoadFromFile loads models JSON from a file (optional).
func (c *ModelsCache) LoadFromFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	// Validate JSON (Copilot API returns an object, not an array)
	var js interface{}
	if err := json.Unmarshal(data, &js); err != nil {
		return fmt.Errorf("invalid models JSON: %w", err)
	}
	c.mu.Lock()
	c.modelsJSON = data
	c.lastFetch = time.Now()
	c.mu.Unlock()
	return nil
}
