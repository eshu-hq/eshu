// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package vaultapi

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
)

// kubernetesAuthMethod is the Vault auth method type whose roles carry the
// bound ServiceAccount selectors used as the IAM-Vault join anchor.
const kubernetesAuthMethod = "kubernetes"

// mountInfo is the shared sys/auth and sys/mounts entry shape.
type mountInfo struct {
	Type     string `json:"type"`
	Accessor string `json:"accessor"`
	Local    bool   `json:"local"`
	Config   struct {
		DefaultLeaseTTL json.RawMessage `json:"default_lease_ttl"`
		MaxLeaseTTL     json.RawMessage `json:"max_lease_ttl"`
	} `json:"config"`
	Options map[string]string `json:"options"`
}

func (a *Adapter) listMounts(ctx context.Context, path string) (map[string]mountInfo, error) {
	var payload struct {
		Data map[string]mountInfo `json:"data"`
	}
	if _, err := a.doRequest(ctx, path, false, &payload); err != nil {
		return nil, err
	}
	return payload.Data, nil
}

// ListAuthMounts returns auth method mount metadata from sys/auth.
func (a *Adapter) ListAuthMounts(ctx context.Context) (_ []vaultlive.AuthMount, err error) {
	defer func() { a.recordAPICall("list_auth_mounts", err) }()
	mounts, err := a.listMounts(ctx, "sys/auth")
	if err != nil {
		return nil, err
	}
	out := make([]vaultlive.AuthMount, 0, len(mounts))
	for path, info := range mounts {
		out = append(out, vaultlive.AuthMount{
			Path: path, Accessor: info.Accessor, Method: info.Type, Local: info.Local,
			DefaultLeaseTTLSeconds: durationSeconds(info.Config.DefaultLeaseTTL),
			MaxLeaseTTLSeconds:     durationSeconds(info.Config.MaxLeaseTTL),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ListSecretEngineMounts returns secret-engine mount metadata from sys/mounts.
func (a *Adapter) ListSecretEngineMounts(ctx context.Context) (_ []vaultlive.SecretEngineMount, err error) {
	defer func() { a.recordAPICall("list_secret_engine_mounts", err) }()
	mounts, err := a.listMounts(ctx, "sys/mounts")
	if err != nil {
		return nil, err
	}
	out := make([]vaultlive.SecretEngineMount, 0, len(mounts))
	for path, info := range mounts {
		out = append(out, vaultlive.SecretEngineMount{
			MountPath: path, MountAccessor: info.Accessor, MountType: info.Type, Local: info.Local,
			KVVersion:              info.Options["version"],
			DefaultLeaseTTLSeconds: durationSeconds(info.Config.DefaultLeaseTTL),
			MaxLeaseTTLSeconds:     durationSeconds(info.Config.MaxLeaseTTL),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MountPath < out[j].MountPath })
	return out, nil
}

// ListACLPolicies returns ACL policy metadata. The raw policy body is hashed,
// never stored. Per-rule capability summaries (ACLPolicy.Rules) are left empty
// in this slice — parsing the Vault ACL HCL/JSON policy grammar is deferred to
// the #1356 runtime-wiring follow-up; until then the policy name plus content
// hash are the emitted posture evidence (downstream emits no per-rule summary).
func (a *Adapter) ListACLPolicies(ctx context.Context) (_ []vaultlive.ACLPolicy, err error) {
	defer func() { a.recordAPICall("list_acl_policies", err) }()
	names, err := a.listKeys(ctx, "sys/policies/acl")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]vaultlive.ACLPolicy, 0, len(names))
	for _, name := range names {
		var payload struct {
			Data struct {
				Name   string `json:"name"`
				Policy string `json:"policy"`
			} `json:"data"`
		}
		ok, err := a.doRequest(ctx, "sys/policies/acl/"+pathEscape(name), false, &payload)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, vaultlive.ACLPolicy{PolicyName: name, PolicyHash: hashPolicyBody(payload.Data.Policy)})
	}
	return out, nil
}

// ListAuthRoles returns auth role metadata for Kubernetes auth mounts (the
// IAM-Vault join anchor). Other auth methods are skipped; their role shapes do
// not carry the ServiceAccount selectors the trust chain needs.
func (a *Adapter) ListAuthRoles(ctx context.Context) (_ []vaultlive.AuthRole, err error) {
	defer func() { a.recordAPICall("list_auth_roles", err) }()
	mounts, err := a.ListAuthMounts(ctx)
	if err != nil {
		return nil, err
	}
	var out []vaultlive.AuthRole
	for _, mount := range mounts {
		if mount.Method != kubernetesAuthMethod {
			continue
		}
		base := "auth/" + strings.TrimRight(mount.Path, "/") + "/role"
		names, err := a.listKeys(ctx, base)
		if err != nil {
			return nil, err
		}
		sort.Strings(names)
		for _, name := range names {
			var payload struct {
				Data struct {
					BoundSANames      []string        `json:"bound_service_account_names"`
					BoundSANamespaces []string        `json:"bound_service_account_namespaces"`
					TokenPolicies     []string        `json:"token_policies"`
					TokenTTL          json.RawMessage `json:"token_ttl"`
				} `json:"data"`
			}
			ok, err := a.doRequest(ctx, base+"/"+pathEscape(name), false, &payload)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			out = append(out, vaultlive.AuthRole{
				MountPath: mount.Path, RoleName: name, Method: mount.Method,
				BoundServiceAccountNames:      payload.Data.BoundSANames,
				BoundServiceAccountNamespaces: payload.Data.BoundSANamespaces,
				TokenPolicyNames:              payload.Data.TokenPolicies,
				TokenTTLSeconds:               durationSeconds(payload.Data.TokenTTL),
			})
		}
	}
	return out, nil
}

// ListIdentityEntities returns identity entity metadata.
func (a *Adapter) ListIdentityEntities(ctx context.Context) (_ []vaultlive.IdentityEntity, err error) {
	defer func() { a.recordAPICall("list_identity_entities", err) }()
	ids, err := a.listKeys(ctx, "identity/entity/id")
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	out := make([]vaultlive.IdentityEntity, 0, len(ids))
	for _, id := range ids {
		var payload struct {
			Data struct {
				Name     string   `json:"name"`
				Aliases  []any    `json:"aliases"`
				GroupIDs []string `json:"group_ids"`
				Disabled bool     `json:"disabled"`
			} `json:"data"`
		}
		ok, err := a.doRequest(ctx, "identity/entity/id/"+pathEscape(id), false, &payload)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, vaultlive.IdentityEntity{
			EntityID: id, EntityName: payload.Data.Name,
			AliasCount: len(payload.Data.Aliases), GroupCount: len(payload.Data.GroupIDs),
			Disabled: payload.Data.Disabled,
		})
	}
	return out, nil
}

// ListIdentityAliases returns identity alias metadata and mount/entity anchors.
func (a *Adapter) ListIdentityAliases(ctx context.Context) (_ []vaultlive.IdentityAlias, err error) {
	defer func() { a.recordAPICall("list_identity_aliases", err) }()
	ids, err := a.listKeys(ctx, "identity/entity-alias/id")
	if err != nil {
		return nil, err
	}
	sort.Strings(ids)
	out := make([]vaultlive.IdentityAlias, 0, len(ids))
	for _, id := range ids {
		var payload struct {
			Data struct {
				CanonicalID   string `json:"canonical_id"`
				MountAccessor string `json:"mount_accessor"`
				MountPath     string `json:"mount_path"`
				Name          string `json:"name"`
			} `json:"data"`
		}
		ok, err := a.doRequest(ctx, "identity/entity-alias/id/"+pathEscape(id), false, &payload)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, vaultlive.IdentityAlias{
			AliasID: id, EntityID: payload.Data.CanonicalID, MountPath: payload.Data.MountPath,
			MountAccessor: payload.Data.MountAccessor, AliasName: payload.Data.Name,
		})
	}
	return out, nil
}
