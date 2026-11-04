package services

import (
	"slices"
	"testing"
)

// TestServiceSiteScopesCarryAdminFlowScopes pins the scopes the admin flows
// of HRPAuth-Yggdrasil-API rely on: the built-in super client is provisioned
// from serviceSiteScopes(), so dropping either entry would silently break the
// Velocity plugin's claim/force-bind calls.
func TestServiceSiteScopesCarryAdminFlowScopes(t *testing.T) {
	scopes := serviceSiteScopes()
	for _, want := range []string{"user.claim.as-service", "user.force-bind.as-service"} {
		if !slices.Contains(scopes, want) {
			t.Errorf("serviceSiteScopes missing %q", want)
		}
	}
}
