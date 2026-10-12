package clients

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/registry"
)

type YggdrasilClient struct {
	BaseURL    string
	HTTPClient *http.Client
}

func NewYggdrasilClient() *YggdrasilClient {
	return &YggdrasilClient{
		BaseURL: config.AppConfig.YggdrasilAPI.BaseURL,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *YggdrasilClient) getInternalKey() string {
	if registry.GlobalRegistry == nil {
		return ""
	}
	// We use the super client ID as the service name for Yggdrasil API
	record, exists := registry.GlobalRegistry.Get(config.AppConfig.OAuth2.SuperClientID)
	if !exists {
		return ""
	}
	return record.InternalKey
}

func (c *YggdrasilClient) SyncUsername(coreUserID, newUsername string) error {
	if c.BaseURL == "" {
		return nil // Yggdrasil API not configured
	}

	url := fmt.Sprintf("%s/internal/sync-username", c.BaseURL)
	body := map[string]string{
		"core_user_id": coreUserID,
		"new_username": newUsername,
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", c.getInternalKey())

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("yggdrasil api returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *YggdrasilClient) ClaimAccount(mojangUUID, coreUserID string) error {
	if c.BaseURL == "" {
		return nil
	}

	url := fmt.Sprintf("%s/internal/claim-account", c.BaseURL)
	body := map[string]string{
		"mojang_uuid":  mojangUUID,
		"core_user_id": coreUserID,
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", c.getInternalKey())

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("yggdrasil api returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *YggdrasilClient) DeleteAccount(coreUserID string) error {
	if c.BaseURL == "" {
		return nil
	}

	url := fmt.Sprintf("%s/internal/delete-account", c.BaseURL)
	body := map[string]string{
		"core_user_id": coreUserID,
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Key", c.getInternalKey())

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("yggdrasil api returned status %d", resp.StatusCode)
	}

	return nil
}

func (c *YggdrasilClient) InvalidateTokens(coreUserID string) error {
        if c.BaseURL == "" {
                return nil
        }

        url := fmt.Sprintf("%s/internal/invalidate-tokens", c.BaseURL)
        body := map[string]string{
                "core_user_id": coreUserID,
        }

        jsonBody, _ := json.Marshal(body)
        req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
        if err != nil {
                return err
        }

        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("X-Internal-Key", c.getInternalKey())

        resp, err := c.HTTPClient.Do(req)
        if err != nil {
                return err
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
                return fmt.Errorf("yggdrasil api returned status %d", resp.StatusCode)
        }

        return nil
}
