package vaultlive

import (
	"context"
	"fmt"

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
// envelopes for the target scope. It performs no graph writes and never reads a
// secret value.
func (s Source) Collect(ctx context.Context, target VaultTarget, client Client) ([]facts.Envelope, error) {
	if client == nil {
		return nil, fmt.Errorf("vault client is required")
	}
	vaultCtx := s.vaultContext(target)

	mounts, err := client.ListAuthMounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vault auth mounts: %w", err)
	}

	envelopes := make([]facts.Envelope, 0, len(mounts))
	for _, mount := range mounts {
		envelope, err := secretsiam.NewVaultAuthMountEnvelope(secretsiam.VaultAuthMountObservation{
			Context:                vaultCtx,
			MountPath:              mount.Path,
			MountAccessor:          mount.Accessor,
			AuthMethod:             mount.Method,
			Local:                  mount.Local,
			DefaultLeaseTTLSeconds: mount.DefaultLeaseTTLSeconds,
			MaxLeaseTTLSeconds:     mount.MaxLeaseTTLSeconds,
			SourceURI:              target.SourceURI,
		})
		if err != nil {
			return nil, fmt.Errorf("build vault auth mount fact: %w", err)
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

// vaultContext builds the secretsiam VaultContext for the target scope.
func (s Source) vaultContext(target VaultTarget) secretsiam.VaultContext {
	return secretsiam.VaultContext{
		VaultClusterID:      target.VaultClusterID,
		Namespace:           target.Namespace,
		ScopeID:             target.ScopeID,
		GenerationID:        target.GenerationID,
		CollectorInstanceID: s.CollectorInstanceID,
		FencingToken:        target.FencingToken,
		ObservedAt:          target.ObservedAt,
		SourceURI:           target.SourceURI,
	}
}
