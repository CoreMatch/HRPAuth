package controllers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/lnb/HRPAuth-Backend-Go/config"
)

func TestBonjourRegistersPresence(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousConfig := config.AppConfig
	config.AppConfig = &config.Config{}
	defer func() {
		config.AppConfig = previousConfig
	}()

	registry := NewPresenceRegistry()
	controller := NewPresenceController(registry)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	body := `{"name":"texture-service","ttl_seconds":120,"scope":{"name":"texture-processing","frontend_areas":["skin","user"]},"security_level":1}`
	req := httptest.NewRequest(http.MethodPost, "/services/presence", newReader(body))
	req.Header.Set("Content-Type", "application/json")
	ctx.Request = req

	controller.Bonjour(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	record, ok := registry.Get("texture-service")
	if !ok {
		t.Fatalf("expected texture-service to be registered")
	}
	if record.SecurityLevel != 1 {
		t.Fatalf("expected security_level 1, got %d", record.SecurityLevel)
	}
	if record.Scope == nil || len(record.Scope.FrontendAreas) != 2 {
		t.Fatalf("expected scope with 2 frontend areas, got %+v", record.Scope)
	}
}

func newReader(s string) *strings.Reader {
	return strings.NewReader(s)
}
