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

// TestOrphanScopesStayRemoved guards the 2026-11-05 cleanup: these scopes had
// no consuming endpoint and were removed from the registry; re-adding them
// requires a consumer to exist first.
func TestOrphanScopesStayRemoved(t *testing.T) {
	all := append([]string{}, publicSiteScopes()...)
	all = append(all, serviceSiteScopes()...)
	all = append(all, targetedServiceScopes()...)
	for _, banned := range []string{"register.manage", "profile.change-name", "profile.change-name.as-service"} {
		if slices.Contains(all, banned) {
			t.Errorf("orphan scope %q re-appeared in the registry", banned)
		}
	}
}
