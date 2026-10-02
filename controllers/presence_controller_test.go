package controllers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
)

func TestPresenceRegistryFrontendSDKs(t *testing.T) {
	registry := NewPresenceRegistry()
	registry.Register("texture-service", 120, &PresenceScope{
		Name:          "texture-processing",
		FrontendAreas: []string{"skin", "user"},
	}, "http://127.0.0.1:2703/sdk/texture-sdk.js", 1, nil)
	registry.Register("audit-service", 120, &PresenceScope{
		Name: "audit",
	}, "", 1, nil)
	registry.Register("internal-service", 120, nil, "http://127.0.0.1:2704/sdk/internal.js", 1, nil)
	registry.Register("profile-service", 120, &PresenceScope{
		Name:          "profile",
		FrontendAreas: []string{"user"},
	}, "http://127.0.0.1:2705/sdk/profile-sdk.js", 1, nil)

	services := registry.FrontendSDKs()
	if len(services) != 2 {
		t.Fatalf("expected 2 frontend sdk services, got %d", len(services))
	}

	if services[0].Name != "profile-service" || services[1].Name != "texture-service" {
		t.Fatalf("expected services sorted by name, got %+v", services)
	}
	if len(services[0].FrontendAreas) != 1 || services[0].FrontendAreas[0] != "user" {
		t.Fatalf("expected profile-service frontend_areas to be preserved, got %+v", services[0].FrontendAreas)
	}
}

func TestListFrontendServicesDoesNotRequirePresenceRegistration(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousConfig := config.AppConfig
	config.AppConfig = &config.Config{
		Callback: config.CallbackConfig{URL: "https://auth.example.com/"},
	}
	defer func() {
		config.AppConfig = previousConfig
	}()

	registry := NewPresenceRegistry()
	registry.Register("texture-service", 120, &PresenceScope{
		Name:          "texture-processing",
		FrontendAreas: []string{"skin", "user"},
	}, "http://127.0.0.1:2703/sdk/texture-sdk.js", 1, nil)

	controller := NewPresenceController(registry)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodGet, "/services/list", nil)
	ctx.Request = req

	controller.ListFrontendServices(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Success bool `json:"success"`
		Data    []struct {
			Name          string   `json:"name"`
			ScopeName     string   `json:"scope_name"`
			FrontendAreas []string `json:"frontend_areas"`
			SDKURL        string   `json:"sdk_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if !payload.Success {
		t.Fatalf("expected success response, got body %s", recorder.Body.String())
	}
	if len(payload.Data) != 1 {
		t.Fatalf("expected 1 service, got %d", len(payload.Data))
	}
	if payload.Data[0].SDKURL != "https://auth.example.com/services/sdk/texture-service" {
		t.Fatalf("expected relayed sdk_url, got %q", payload.Data[0].SDKURL)
	}
	if len(payload.Data[0].FrontendAreas) != 2 || payload.Data[0].FrontendAreas[0] != "skin" || payload.Data[0].FrontendAreas[1] != "user" {
		t.Fatalf("expected frontend_areas in response, got %+v", payload.Data[0].FrontendAreas)
	}
}
