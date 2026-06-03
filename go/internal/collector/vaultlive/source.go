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
// Collection is per-family resilient: a single family's list failure (for
// example a permission-scoped read) emits a redacted vault_coverage_warning
// fact (source_state=partial, resource_scope=<family>) and collection
// continues, so one denied family does not lose the whole generation. The
// partial state is therefore explicit in the facts, never silently complete.
// Context cancellation and a malformed observation (a build error) remain fatal.
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
	var err error

	if envelopes, err = collectFamily(ctx, envelopes, vaultCtx, uri, vaultFamilyAuthMounts,
		func() ([]AuthMount, error) { return client.ListAuthMounts(ctx) },
		func(m AuthMount) (facts.Envelope, error) { return mapAuthMount(vaultCtx, uri, m) }); err != nil {
		return nil, err
	}
	if envelopes, err = collectFamily(ctx, envelopes, vaultCtx, uri, vaultFamilyAuthRoles,
		func() ([]AuthRole, error) { return client.ListAuthRoles(ctx) },
		func(r AuthRole) (facts.Envelope, error) { return mapAuthRole(vaultCtx, uri, r) }); err != nil {
		return nil, err
	}
	if envelopes, err = collectFamily(ctx, envelopes, vaultCtx, uri, vaultFamilyACLPolicies,
		func() ([]ACLPolicy, error) { return client.ListACLPolicies(ctx) },
		func(p ACLPolicy) (facts.Envelope, error) { return mapACLPolicy(vaultCtx, uri, p) }); err != nil {
		return nil, err
	}
	if envelopes, err = collectFamily(ctx, envelopes, vaultCtx, uri, vaultFamilyIdentityEntities,
		func() ([]IdentityEntity, error) { return client.ListIdentityEntities(ctx) },
		func(e IdentityEntity) (facts.Envelope, error) { return mapIdentityEntity(vaultCtx, uri, e) }); err != nil {
		return nil, err
	}
	if envelopes, err = collectFamily(ctx, envelopes, vaultCtx, uri, vaultFamilyIdentityAliases,
		func() ([]IdentityAlias, error) { return client.ListIdentityAliases(ctx) },
		func(a IdentityAlias) (facts.Envelope, error) { return mapIdentityAlias(vaultCtx, uri, a) }); err != nil {
		return nil, err
	}
	if envelopes, err = collectFamily(ctx, envelopes, vaultCtx, uri, vaultFamilyKVMetadata,
		func() ([]KVMetadata, error) { return client.ListKVMetadata(ctx) },
		func(m KVMetadata) (facts.Envelope, error) { return mapKVMetadata(vaultCtx, uri, m) }); err != nil {
		return nil, err
	}
	if envelopes, err = collectFamily(ctx, envelopes, vaultCtx, uri, vaultFamilySecretEngineMounts,
		func() ([]SecretEngineMount, error) { return client.ListSecretEngineMounts(ctx) },
		func(m SecretEngineMount) (facts.Envelope, error) { return mapSecretEngineMount(vaultCtx, uri, m) }); err != nil {
		return nil, err
	}

	return envelopes, nil
}

// Vault fact-family identifiers. They are the bounded resource_scope / reason
// labels for coverage warnings and the partial-scope counter — never a path or
// secret.
const (
	vaultFamilyAuthMounts         = "auth_mounts"
	vaultFamilyAuthRoles          = "auth_roles"
	vaultFamilyACLPolicies        = "acl_policies"
	vaultFamilyIdentityEntities   = "identity_entities"
	vaultFamilyIdentityAliases    = "identity_aliases"
	vaultFamilyKVMetadata         = "kv_metadata"
	vaultFamilySecretEngineMounts = "secret_engine_mounts"
)

// collectFamily lists one Vault fact family and maps it to envelopes. A list
// error emits a redacted vault_coverage_warning (resource_scope=family) and
// returns it instead of failing the whole generation, except for context
// cancellation, which is fatal. A build (mapping) error is fatal — it signals a
// malformed observation, not a source-coverage gap.
func collectFamily[T any](
	ctx context.Context,
	dst []facts.Envelope,
	vaultCtx secretsiam.VaultContext,
	uri string,
	family string,
	list func() ([]T, error),
	build func(T) (facts.Envelope, error),
) ([]facts.Envelope, error) {
	items, err := list()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("list vault %s: %w", family, ctx.Err())
		}
		warning, werr := secretsiam.NewVaultCoverageWarningEnvelope(secretsiam.VaultCoverageWarningObservation{
			Context:       vaultCtx,
			WarningKind:   "partial_family",
			SourceState:   secretsiam.SourceStatePartial,
			ResourceScope: family,
			ErrorClass:    "list_failed",
			SourceURI:     uri,
			// Message is intentionally omitted: a raw Vault error can carry the
			// path or address, so only the bounded family/class is recorded.
		})
		if werr != nil {
			return nil, fmt.Errorf("build vault %s coverage warning: %w", family, werr)
		}
		return append(dst, warning), nil
	}
	return collectInto(dst, family, items, build)
}

// payloadStringValue reads a string fact-payload field, returning "" when
// absent or not a string.
func payloadStringValue(payload map[string]any, key string) string {
	if v, ok := payload[key].(string); ok {
		return v
	}
	return ""
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
