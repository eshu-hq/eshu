package oidclogin

import (
	"context"
	"errors"
)

// GroupRoleMapping maps an external IdP group to Eshu role identifiers.
type GroupRoleMapping struct {
	Group       string   `json:"group,omitempty"`
	GroupSHA256 string   `json:"group_sha256,omitempty"`
	RoleIDs     []string `json:"role_ids"`
}

// RoleGrant maps an Eshu role to concrete session grants.
type RoleGrant struct {
	RoleID               string   `json:"role_id"`
	PolicyRevisionHash   string   `json:"policy_revision_hash"`
	AllScopes            bool     `json:"all_scopes,omitempty"`
	AllowedScopeIDs      []string `json:"allowed_scope_ids,omitempty"`
	AllowedRepositoryIDs []string `json:"allowed_repository_ids,omitempty"`
}

// StaticGrantResolver resolves group hashes through a private config file.
type StaticGrantResolver struct {
	GroupRoleMappings []GroupRoleMapping `json:"group_role_mappings"`
	RoleGrants        []RoleGrant        `json:"role_grants"`
}

// ResolveGroupGrants maps external group hashes to Eshu roles, then roles to
// concrete grants. It never treats a raw group as a direct permission.
func (r StaticGrantResolver) ResolveGroupGrants(
	_ context.Context,
	query GrantQuery,
) (GrantResolution, bool, error) {
	groupRoles, err := r.groupRoleIndex()
	if err != nil {
		return GrantResolution{}, false, err
	}
	roleGrants, err := r.roleGrantIndex()
	if err != nil {
		return GrantResolution{}, false, err
	}
	roleIDs := make([]string, 0)
	for _, groupHash := range cleanStrings(query.GroupHashes) {
		roleIDs = append(roleIDs, groupRoles[groupHash]...)
	}
	roleIDs = cleanStrings(roleIDs)
	if len(roleIDs) == 0 {
		return GrantResolution{}, false, nil
	}
	resolution := GrantResolution{RoleIDs: roleIDs}
	for _, roleID := range roleIDs {
		grant, ok := roleGrants[roleID]
		if !ok {
			return GrantResolution{}, false, errors.New("role grant is missing")
		}
		if resolution.PolicyRevisionHash == "" {
			resolution.PolicyRevisionHash = grant.PolicyRevisionHash
		} else if resolution.PolicyRevisionHash != grant.PolicyRevisionHash {
			return GrantResolution{}, false, errors.New("role grants have conflicting policy revisions")
		}
		resolution.AllScopes = resolution.AllScopes || grant.AllScopes
		resolution.AllowedScopeIDs = append(resolution.AllowedScopeIDs, grant.AllowedScopeIDs...)
		resolution.AllowedRepositoryIDs = append(resolution.AllowedRepositoryIDs, grant.AllowedRepositoryIDs...)
	}
	resolution.AllowedScopeIDs = cleanStrings(resolution.AllowedScopeIDs)
	resolution.AllowedRepositoryIDs = cleanStrings(resolution.AllowedRepositoryIDs)
	if resolution.PolicyRevisionHash == "" {
		return GrantResolution{}, false, errors.New("role grant policy revision is required")
	}
	return resolution, true, nil
}

func (r StaticGrantResolver) groupRoleIndex() (map[string][]string, error) {
	index := make(map[string][]string, len(r.GroupRoleMappings))
	for _, mapping := range r.GroupRoleMappings {
		groupHash := mapping.GroupSHA256
		if groupHash == "" && mapping.Group != "" {
			groupHash = SHA256Hash(mapping.Group)
		}
		groupHash = cleanScalar(groupHash)
		roleIDs := cleanStrings(mapping.RoleIDs)
		if groupHash == "" || len(roleIDs) == 0 {
			return nil, errors.New("group mapping requires group hash and role ids")
		}
		index[groupHash] = cleanStrings(append(index[groupHash], roleIDs...))
	}
	return index, nil
}

func (r StaticGrantResolver) roleGrantIndex() (map[string]RoleGrant, error) {
	index := make(map[string]RoleGrant, len(r.RoleGrants))
	for _, grant := range r.RoleGrants {
		grant.RoleID = cleanScalar(grant.RoleID)
		grant.PolicyRevisionHash = cleanScalar(grant.PolicyRevisionHash)
		grant.AllowedScopeIDs = cleanStrings(grant.AllowedScopeIDs)
		grant.AllowedRepositoryIDs = cleanStrings(grant.AllowedRepositoryIDs)
		if grant.RoleID == "" || grant.PolicyRevisionHash == "" {
			return nil, errors.New("role grant requires role id and policy revision")
		}
		if _, exists := index[grant.RoleID]; exists {
			return nil, errors.New("role grant role ids must be unique")
		}
		index[grant.RoleID] = grant
	}
	return index, nil
}

func cleanScalar(value string) string {
	values := cleanStrings([]string{value})
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
