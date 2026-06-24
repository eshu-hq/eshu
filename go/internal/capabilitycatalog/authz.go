// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capabilitycatalog

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// AuthorizationFileName is the authorization catalog file inside specs/.
const AuthorizationFileName = "authorization-catalog.v1.yaml"

var requiredPermissionFamilies = []string{
	"identity_admin",
	"roles_grants",
	"tokens",
	"repository_content",
	"service_runtime",
	"cloud_iac",
	"secrets_iam",
	"supply_chain",
	"docs_semantic",
	"ask_search",
	"operations_status",
	"audit_export",
	"admin_recovery",
}

// AuthorizationCatalog defines the built-in role, data-class, and permission
// family model used to enrich every capability entry.
type AuthorizationCatalog struct {
	Version            string               `json:"version" yaml:"version"`
	Roles              []BuiltInRole        `json:"roles" yaml:"roles"`
	DataClasses        []DataClass          `json:"data_classes" yaml:"data_classes"`
	PermissionFamilies []PermissionFamily   `json:"permission_families" yaml:"permission_families"`
	BootstrapOwner     BootstrapOwnerPolicy `json:"bootstrap_owner" yaml:"bootstrap_owner"`
	CustomPolicy       CustomPolicyPosture  `json:"custom_policy" yaml:"custom_policy"`
}

// BuiltInRole is one product role in the v1 authorization model.
type BuiltInRole struct {
	Role             string      `json:"role" yaml:"role"`
	DisplayName      string      `json:"display_name" yaml:"display_name"`
	Description      string      `json:"description,omitempty" yaml:"description"`
	BootstrapDefault bool        `json:"bootstrap_default,omitempty" yaml:"bootstrap_default"`
	Grants           []RoleGrant `json:"grants" yaml:"grants"`
}

// RoleGrant maps a role to an action and optional data classes.
type RoleGrant struct {
	Action      string   `json:"action" yaml:"action"`
	DataClasses []string `json:"data_classes,omitempty" yaml:"data_classes"`
	ScopeLevels []string `json:"scope_levels,omitempty" yaml:"scope_levels"`
}

// DataClass names one class of data that can be granted independently from
// feature or admin power.
type DataClass struct {
	DataClass   string `json:"data_class" yaml:"data_class"`
	Sensitivity string `json:"sensitivity" yaml:"sensitivity"`
	Description string `json:"description" yaml:"description"`
}

// PermissionFamily maps capability id prefixes to a closed action,
// data-class, and default-role grant.
type PermissionFamily struct {
	Family             string   `json:"family" yaml:"family"`
	Description        string   `json:"description,omitempty" yaml:"description"`
	Planned            bool     `json:"planned,omitempty" yaml:"planned"`
	CapabilityPrefixes []string `json:"capability_prefixes" yaml:"capability_prefixes"`
	Action             string   `json:"action" yaml:"action"`
	DataClasses        []string `json:"data_classes,omitempty" yaml:"data_classes"`
	ScopeLevels        []string `json:"scope_levels,omitempty" yaml:"scope_levels"`
	DefaultRoles       []string `json:"default_roles" yaml:"default_roles"`
}

// BootstrapOwnerPolicy records the first-owner grant posture.
type BootstrapOwnerPolicy struct {
	Role                          string   `json:"role" yaml:"role"`
	StartsWithAdmin               bool     `json:"starts_with_admin" yaml:"starts_with_admin"`
	StartsWithSensitiveDataGrants bool     `json:"starts_with_sensitive_data_grants" yaml:"starts_with_sensitive_data_grants"`
	DelegableRoles                []string `json:"delegable_roles" yaml:"delegable_roles"`
}

// CustomPolicyPosture records that v1 intentionally defers custom policy logic
// while leaving the catalog extensible.
type CustomPolicyPosture struct {
	Status string `json:"status" yaml:"status"`
	Note   string `json:"note" yaml:"note"`
}

// CapabilityAuthorization is the explicit grant metadata attached to one
// capability entry in the generated catalog.
type CapabilityAuthorization struct {
	Family        string   `json:"family"`
	Action        string   `json:"action"`
	DataClasses   []string `json:"data_classes,omitempty"`
	ScopeLevels   []string `json:"scope_levels,omitempty"`
	DefaultRoles  []string `json:"default_roles"`
	SensitiveData bool     `json:"sensitive_data"`
}

type authorizationIndex struct {
	enabled               bool
	roles                 map[string]BuiltInRole
	dataClassSensitivity  map[string]string
	families              []PermissionFamily
	permissionFamilyNames map[string]struct{}
}

// LoadAuthorizationCatalog reads the role/grant/data-class authorization
// catalog. The file is required for BuildFromSpecs because every generated
// catalog artifact must carry authorization metadata.
func LoadAuthorizationCatalog(path string) (AuthorizationCatalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return AuthorizationCatalog{}, fmt.Errorf("read authorization catalog %s: %w", path, err)
	}
	var catalog AuthorizationCatalog
	if err := yaml.Unmarshal(raw, &catalog); err != nil {
		return AuthorizationCatalog{}, fmt.Errorf("parse authorization catalog %s: %w", path, err)
	}
	return normalizeAuthorizationCatalog(catalog), nil
}

func normalizeAuthorizationCatalog(catalog AuthorizationCatalog) AuthorizationCatalog {
	sort.Slice(catalog.Roles, func(i, j int) bool { return catalog.Roles[i].Role < catalog.Roles[j].Role })
	for i := range catalog.Roles {
		sort.Slice(catalog.Roles[i].Grants, func(a, b int) bool {
			return catalog.Roles[i].Grants[a].Action < catalog.Roles[i].Grants[b].Action
		})
	}
	sort.Slice(catalog.DataClasses, func(i, j int) bool {
		return catalog.DataClasses[i].DataClass < catalog.DataClasses[j].DataClass
	})
	sort.Slice(catalog.PermissionFamilies, func(i, j int) bool {
		return catalog.PermissionFamilies[i].Family < catalog.PermissionFamilies[j].Family
	})
	return catalog
}

func newAuthorizationIndex(catalog AuthorizationCatalog) authorizationIndex {
	index := authorizationIndex{
		enabled:               authorizationCatalogEnabled(catalog),
		roles:                 map[string]BuiltInRole{},
		dataClassSensitivity:  map[string]string{},
		permissionFamilyNames: map[string]struct{}{},
	}
	for _, role := range catalog.Roles {
		index.roles[role.Role] = role
	}
	for _, dataClass := range catalog.DataClasses {
		index.dataClassSensitivity[dataClass.DataClass] = strings.ToLower(strings.TrimSpace(dataClass.Sensitivity))
	}
	index.families = append(index.families, catalog.PermissionFamilies...)
	for _, family := range index.families {
		index.permissionFamilyNames[family.Family] = struct{}{}
	}
	return index
}

func authorizationCatalogEnabled(catalog AuthorizationCatalog) bool {
	return catalog.Version != "" || len(catalog.Roles) > 0 || len(catalog.PermissionFamilies) > 0
}

func (index authorizationIndex) authorizationFor(capability string) (CapabilityAuthorization, bool) {
	if !index.enabled {
		return CapabilityAuthorization{}, false
	}
	var best PermissionFamily
	bestPrefixLength := -1
	for _, family := range index.families {
		for _, prefix := range family.CapabilityPrefixes {
			if strings.HasPrefix(capability, prefix) && len(prefix) > bestPrefixLength {
				best = family
				bestPrefixLength = len(prefix)
			}
		}
	}
	if bestPrefixLength < 0 {
		return CapabilityAuthorization{}, false
	}
	return CapabilityAuthorization{
		Family:        best.Family,
		Action:        best.Action,
		DataClasses:   append([]string(nil), best.DataClasses...),
		ScopeLevels:   append([]string(nil), best.ScopeLevels...),
		DefaultRoles:  append([]string(nil), best.DefaultRoles...),
		SensitiveData: index.hasSensitiveData(best.DataClasses),
	}, true
}

func (index authorizationIndex) hasSensitiveData(dataClasses []string) bool {
	for _, dataClass := range dataClasses {
		if index.dataClassSensitivity[dataClass] == "sensitive" {
			return true
		}
	}
	return false
}

func authorizationCatalogHasFamily(catalog AuthorizationCatalog, family string) bool {
	for _, candidate := range catalog.PermissionFamilies {
		if candidate.Family == family {
			return true
		}
	}
	return false
}

func roleHasAction(catalog AuthorizationCatalog, roleID, action string) bool {
	for _, role := range catalog.Roles {
		if role.Role != roleID {
			continue
		}
		for _, grant := range role.Grants {
			if grant.Action == action {
				return true
			}
		}
	}
	return false
}

func roleCoversPermissionFamily(role BuiltInRole, family PermissionFamily) bool {
	grantDataClasses := map[string]struct{}{}
	grantScopeLevels := map[string]struct{}{}
	actionMatched := false
	for _, grant := range role.Grants {
		if grant.Action != family.Action {
			continue
		}
		actionMatched = true
		for _, dataClass := range grant.DataClasses {
			grantDataClasses[dataClass] = struct{}{}
		}
		for _, scopeLevel := range grant.ScopeLevels {
			grantScopeLevels[scopeLevel] = struct{}{}
		}
	}
	if !actionMatched {
		return false
	}
	return stringSetContainsAll(grantDataClasses, family.DataClasses) &&
		stringSetContainsAll(grantScopeLevels, family.ScopeLevels)
}

func stringSetContainsAll(have map[string]struct{}, wants []string) bool {
	for _, want := range wants {
		if _, ok := have[want]; !ok {
			return false
		}
	}
	return true
}

func roleHasSensitiveDataGrant(catalog AuthorizationCatalog, roleID string) bool {
	index := newAuthorizationIndex(catalog)
	role, ok := index.roles[roleID]
	if !ok {
		return false
	}
	for _, grant := range role.Grants {
		if index.hasSensitiveData(grant.DataClasses) {
			return true
		}
	}
	return false
}
