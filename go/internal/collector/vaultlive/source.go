package vaultlive

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Source maps read-only Vault metadata into redacted secretsiam source facts
// for one bounded scan scope. It holds no Vault credentials; the caller injects
// an already-authenticated read-only Client.
type Source struct {
	// CollectorInstanceID identifies the collector instance for fact provenance.
	CollectorInstanceID string
}

// Collect reads metadata from the Vault Client and returns redacted source-fact
// envelopes for the target scope, covering all seven Vault metadata fact
// families. It performs no graph writes and never reads a secret value.
//
// Collection is fail-fast per family: a read error is wrapped and returned so a
// partial generation is never silently emitted as if complete. (Per-family
// partial-coverage warnings are added with the live adapter in #1356.)
func (s Source) Collect(ctx context.Context, target VaultTarget, client Client) ([]facts.Envelope, error) {
	if client == nil {
		return nil, fmt.Errorf("vault client is required")
	}
	// Sanitize the Vault endpoint URL once: a credential-bearing address
	// (basic-auth userinfo or a token query param) must never reach a fact's
	// SourceRef. Every family and the context use the sanitized form.
	uri := sanitizeVaultSourceURI(target.SourceURI)
	vaultCtx := s.vaultContext(target, uri)
	var envelopes []facts.Envelope

	authMounts, err := client.ListAuthMounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault auth mounts: %w", err)
	}
	if envelopes, err = collectInto(envelopes, "vault auth mount", authMounts, func(m AuthMount) (facts.Envelope, error) {
		return mapAuthMount(vaultCtx, uri, m)
	}); err != nil {
		return nil, err
	}

	authRoles, err := client.ListAuthRoles(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault auth roles: %w", err)
	}
	if envelopes, err = collectInto(envelopes, "vault auth role", authRoles, func(r AuthRole) (facts.Envelope, error) {
		return mapAuthRole(vaultCtx, uri, r)
	}); err != nil {
		return nil, err
	}

	aclPolicies, err := client.ListACLPolicies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault acl policies: %w", err)
	}
	if envelopes, err = collectInto(envelopes, "vault acl policy", aclPolicies, func(p ACLPolicy) (facts.Envelope, error) {
		return mapACLPolicy(vaultCtx, uri, p)
	}); err != nil {
		return nil, err
	}

	entities, err := client.ListIdentityEntities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault identity entities: %w", err)
	}
	if envelopes, err = collectInto(envelopes, "vault identity entity", entities, func(e IdentityEntity) (facts.Envelope, error) {
		return mapIdentityEntity(vaultCtx, uri, e)
	}); err != nil {
		return nil, err
	}

	aliases, err := client.ListIdentityAliases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault identity aliases: %w", err)
	}
	if envelopes, err = collectInto(envelopes, "vault identity alias", aliases, func(a IdentityAlias) (facts.Envelope, error) {
		return mapIdentityAlias(vaultCtx, uri, a)
	}); err != nil {
		return nil, err
	}

	kvMetadata, err := client.ListKVMetadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault kv metadata: %w", err)
	}
	if envelopes, err = collectInto(envelopes, "vault kv metadata", kvMetadata, func(m KVMetadata) (facts.Envelope, error) {
		return mapKVMetadata(vaultCtx, uri, m)
	}); err != nil {
		return nil, err
	}

	engineMounts, err := client.ListSecretEngineMounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault secret engine mounts: %w", err)
	}
	if envelopes, err = collectInto(envelopes, "vault secret engine mount", engineMounts, func(m SecretEngineMount) (facts.Envelope, error) {
		return mapSecretEngineMount(vaultCtx, uri, m)
	}); err != nil {
		return nil, err
	}

	return envelopes, nil
}

// collectInto builds one envelope per item via build and appends it to dst,
// wrapping a build error with the fact-family label so a malformed observation
// is diagnosable.
func collectInto[T any](
	dst []facts.Envelope,
	family string,
	items []T,
	build func(T) (facts.Envelope, error),
) ([]facts.Envelope, error) {
	dst = slices.Grow(dst, len(items))
	for _, item := range items {
		envelope, err := build(item)
		if err != nil {
			return nil, fmt.Errorf("build %s fact: %w", family, err)
		}
		dst = append(dst, envelope)
	}
	return dst, nil
}

// vaultContext builds the secretsiam VaultContext for the target scope. The
// caller passes an already-sanitized sourceURI so no credential-bearing Vault
// address reaches the fact context.
func (s Source) vaultContext(target VaultTarget, sourceURI string) secretsiam.VaultContext {
	return secretsiam.VaultContext{
		VaultClusterID:      target.VaultClusterID,
		Namespace:           target.Namespace,
		ScopeID:             target.ScopeID,
		GenerationID:        target.GenerationID,
		CollectorInstanceID: s.CollectorInstanceID,
		FencingToken:        target.FencingToken,
		ObservedAt:          target.ObservedAt,
		SourceURI:           sourceURI,
	}
}

// sanitizeVaultSourceURI returns a provenance-safe form of a Vault endpoint URL
// with any userinfo, query, and fragment removed, so a credential-bearing Vault
// address (basic-auth userinfo or token query parameters) can never be
// persisted in a fact's SourceRef. It returns "" when the value is not a
// parseable absolute URL, so an unexpected shape is omitted rather than leaked.
func sanitizeVaultSourceURI(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	safe := url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}
	return safe.String()
}
