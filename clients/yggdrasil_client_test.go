package clients

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lnb/HRPAuth-Backend-Go/config"
	"github.com/lnb/HRPAuth-Backend-Go/registry"
)

func TestInvalidateTokensPostsExpectedPayload(t *testing.T) {
	previousConfig := config.AppConfig
	config.AppConfig = &config.Config{
		OAuth2: config.OAuth2Config{
			SuperClientID: "hrpauth-internal-super",
		},
	}
	previousRegistry := registry.GlobalRegistry
	registry.GlobalRegistry = registry.NewPresenceRegistry()
	registry.GlobalRegistry.Register("hrpauth-internal-super", 0, nil, 0, nil)
	registry.GlobalRegistry.RegisterAuthKey("hrpauth-internal-super")
	record, _ := registry.GlobalRegistry.Get("hrpauth-internal-super")
	testKey := record.InternalKey

	defer func() {
		config.AppConfig = previousConfig
		registry.GlobalRegistry = previousRegistry
	}()

	var (
		gotPath        string
		gotInternalKey string
		gotBody        map[string]string
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotInternalKey = r.Header.Get("X-Internal-Key")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &YggdrasilClient{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	if err := client.InvalidateTokens("core-user-123"); err != nil {
		t.Fatalf("InvalidateTokens returned error: %v", err)
	}

	if gotPath != "/internal/invalidate-tokens" {
		t.Fatalf("expected request path /internal/invalidate-tokens, got %q", gotPath)
	}
	if gotInternalKey != testKey {
		t.Fatalf("expected internal key header to be forwarded, got %q", gotInternalKey)
	}
	if gotBody["core_user_id"] != "core-user-123" {
		t.Fatalf("expected core_user_id payload, got %v", gotBody)
	}
}

func TestInvalidateTokensAcceptsNotFoundAsIdempotentSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	client := &YggdrasilClient{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	}

	if err := client.InvalidateTokens("missing-core-user"); err != nil {
		t.Fatalf("expected 404 to be treated as idempotent success, got %v", err)
	}
}
