// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semanticpolicy

import (
	"slices"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func normalizeRequest(request Request) Request {
	return Request{
		ProviderProfileID: strings.TrimSpace(request.ProviderProfileID),
		SourceClass:       strings.TrimSpace(request.SourceClass),
		Scope: Scope{
			Kind: strings.TrimSpace(request.Scope.Kind),
			ID:   strings.TrimSpace(request.Scope.ID),
		},
		SourceID:      strings.TrimSpace(request.SourceID),
		DocumentID:    strings.TrimSpace(request.DocumentID),
		SourceURIHash: strings.TrimSpace(request.SourceURIHash),
		SourcePath:    cleanSourcePath(request.SourcePath),
		ACLState:      strings.TrimSpace(request.ACLState),
	}
}

func deny(state, reason, detail string, request Request) Decision {
	return Decision{
		Allowed:           false,
		State:             state,
		Reason:            reason,
		Detail:            detail,
		ProviderProfileID: strings.TrimSpace(request.ProviderProfileID),
		SourceClass:       strings.TrimSpace(request.SourceClass),
	}
}

func profileAllowsRequest(
	profiles []status.SemanticProviderProfileStatus,
	request Request,
) bool {
	for _, profile := range profiles {
		if strings.TrimSpace(profile.ProfileID) != request.ProviderProfileID {
			continue
		}
		if profile.State == status.SemanticProviderProfileUnhealthy ||
			!profile.CredentialConfigured {
			return false
		}
		return slices.Contains(normalizedStrings(profile.SourceClasses), request.SourceClass)
	}
	return false
}

func ruleMatchesProfileAndClass(rule Rule, request Request) bool {
	return rule.ProviderProfileID == request.ProviderProfileID &&
		slices.Contains(rule.SourceClasses, request.SourceClass)
}

func scopeMatches(scopes []Scope, request Scope) bool {
	request = Scope{Kind: strings.TrimSpace(request.Kind), ID: strings.TrimSpace(request.ID)}
	for _, scope := range scopes {
		if scope.Kind == request.Kind && scope.ID == request.ID {
			return true
		}
	}
	return false
}

func sourceMatches(selectors []SourceSelector, request Request) bool {
	for _, selector := range selectors {
		switch selector.Kind {
		case SourceSelectorAll:
			return true
		case SourceSelectorPathPrefix:
			if request.SourcePath != "" && strings.HasPrefix(request.SourcePath, cleanSourcePath(selector.Value)) {
				return true
			}
		case SourceSelectorSourceID:
			if request.SourceID != "" && request.SourceID == selector.Value {
				return true
			}
		case SourceSelectorDocumentID:
			if request.DocumentID != "" && request.DocumentID == selector.Value {
				return true
			}
		case SourceSelectorSourceURIHash:
			if request.SourceURIHash != "" && request.SourceURIHash == selector.Value {
				return true
			}
		}
	}
	return false
}

func allowedProfileSourceClasses(
	profile status.SemanticProviderProfileStatus,
	policy Policy,
	denied map[string]struct{},
) []string {
	profileClasses := normalizedStrings(profile.SourceClasses)
	allowed := make(map[string]struct{})
	for _, rule := range policy.Rules {
		if rule.ProviderProfileID != strings.TrimSpace(profile.ProfileID) {
			continue
		}
		for _, sourceClass := range rule.SourceClasses {
			if _, blocked := denied[sourceClass]; blocked {
				continue
			}
			if slices.Contains(profileClasses, sourceClass) {
				if !egressAllowsProfileSourceClass(policy.Egress, profile.ProfileID, sourceClass) {
					continue
				}
				allowed[sourceClass] = struct{}{}
			}
		}
	}
	if len(allowed) == 0 {
		return nil
	}
	out := make([]string, 0, len(allowed))
	for sourceClass := range allowed {
		out = append(out, sourceClass)
	}
	sort.Strings(out)
	return out
}

func normalizedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cleanSourcePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "./")
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	return path
}
