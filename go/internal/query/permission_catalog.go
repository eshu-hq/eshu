package query

import (
	"context"
	"net/http"
)

const permissionFeatureAskSearch = "ask_search"

var permissionDataClassesAskSearch = []string{
	"ask_reasoning",
	"source_content",
	"documentation_semantic",
}

func authContextAllowsPermissionFeature(ctx context.Context, feature string) bool {
	auth, ok := AuthContextFromContext(ctx)
	if !ok || !auth.PermissionCatalogEnforced || auth.AllScopes || auth.Mode == AuthModeShared {
		return true
	}
	for _, allowed := range auth.AllowedPermissionFeatures {
		if allowed == feature || allowed == "*" {
			return true
		}
	}
	return false
}

func authContextAllowsPermissionDataClasses(ctx context.Context, dataClasses ...string) bool {
	auth, ok := AuthContextFromContext(ctx)
	if !ok || !auth.PermissionCatalogEnforced || auth.AllScopes || auth.Mode == AuthModeShared {
		return true
	}
	allowed := make(map[string]struct{}, len(auth.AllowedPermissionDataClasses))
	for _, dataClass := range auth.AllowedPermissionDataClasses {
		allowed[dataClass] = struct{}{}
	}
	if _, ok := allowed["*"]; ok {
		return true
	}
	for _, dataClass := range dataClasses {
		if _, ok := allowed[dataClass]; !ok {
			return false
		}
	}
	return true
}

func writePermissionDeniedEnvelope(w http.ResponseWriter, capability string) {
	WriteJSON(w, http.StatusForbidden, ResponseEnvelope{Error: &ErrorEnvelope{
		Code:       ErrorCodePermissionDenied,
		Message:    "permission denied",
		Capability: capability,
	}})
}
