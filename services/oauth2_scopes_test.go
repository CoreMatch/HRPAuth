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

// TestOrphanScopesStayRemoved guards the 2026-11-05 cleanups: these scopes had
// no consuming endpoint and were removed from the registry; re-adding them
// requires a consumer to exist first. Batch 2 covers the Mojang bind scopes
// (the flow moved to the Yggdrasil-API /player endpoints, which do not
// consume core scopes) plus the never-wired totp/email-2fa status scopes.
func TestOrphanScopesStayRemoved(t *testing.T) {
	all := append([]string{}, publicSiteScopes()...)
	all = append(all, serviceSiteScopes()...)
	all = append(all, targetedServiceScopes()...)
	banned := []string{
		"register.manage",
		"profile.change-name",
		"profile.change-name.as-service",
		"user.mojang-bind-enable",
		"user.mojang-bind-enable.as-service",
		"user.mojang-bind-disable.as-service",
		"totp.status",
		"totp.status.as-service",
		"email-2fa.status",
		"email-2fa.status.as-service",
	}
	for _, scope := range banned {
		if slices.Contains(all, scope) {
			t.Errorf("orphan scope %q re-appeared in the registry", scope)
		}
	}
}
