package terraformstate

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

func normalizedParseOptions(options ParseOptions) ParseOptions {
	if options.ObservedAt.IsZero() {
		options.ObservedAt = options.Generation.ObservedAt
	}
	if options.ObservedAt.IsZero() {
		options.ObservedAt = time.Now().UTC()
	}
	options.ObservedAt = options.ObservedAt.UTC()
	return options
}

func resourceAddress(resource resourceContext, instance instanceContext, instanceIndex int) string {
	prefix := ""
	if module := strings.TrimSpace(resource.Module); module != "" {
		prefix = module + "."
	}
	if strings.TrimSpace(resource.Mode) == "data" {
		prefix += "data."
	}
	address := prefix + strings.TrimSpace(resource.Type) + "." + strings.TrimSpace(resource.Name)
	if instance.HasIndexKey {
		return fmt.Sprintf("%s[key:%s]", address, instance.IndexKeyHash)
	}
	if instanceIndex > 0 {
		return fmt.Sprintf("%s[index:%d]", address, instanceIndex)
	}
	return address
}

func validateResourceIdentity(resource resourceContext) error {
	switch strings.TrimSpace(resource.Mode) {
	case "managed", "data":
	default:
		return fmt.Errorf("terraform state resource mode must be managed or data")
	}
	if strings.TrimSpace(resource.Type) == "" {
		return fmt.Errorf("terraform state resource type must not be blank")
	}
	if strings.TrimSpace(resource.Name) == "" {
		return fmt.Errorf("terraform state resource name must not be blank")
	}
	return nil
}

func expectedSnapshotIdentity(freshnessHint string) (string, int64, error) {
	var lineage string
	var serialValue string
	for _, field := range strings.Fields(freshnessHint) {
		if value, ok := strings.CutPrefix(field, "lineage="); ok {
			lineage = value
		}
		if value, ok := strings.CutPrefix(field, "serial="); ok {
			serialValue = value
		}
	}
	if strings.TrimSpace(lineage) == "" {
		return "", 0, fmt.Errorf("terraform state generation freshness hint must include lineage")
	}
	serial, err := strconv.ParseInt(serialValue, 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("terraform state generation freshness hint must include serial: %w", err)
	}
	return lineage, serial, nil
}

func instanceIndexHash(value any) string {
	return facts.StableID("TerraformStateInstanceIndexKey", map[string]any{
		"index_key": value,
	})
}

func redactionMap(value redact.Value) map[string]any {
	return map[string]any{
		"marker": value.Marker,
		"reason": value.Reason,
		"source": value.Source,
	}
}

func sourceURI(source StateKey) string {
	return fmt.Sprintf("terraform_state:%s:%s", source.BackendKind, LocatorHash(source))
}

// LocatorHash returns the per-version durable hash used to identify one
// exact Terraform state candidate (backend, locator, and S3 object version)
// without exposing bucket names, object keys, or local paths.
//
// This hash IS version-aware: it digests BackendKind, Locator, and VersionID.
// It backs the per-candidate planning identity (CandidatePlanningID) and
// the persisted `terraform_state_snapshot.payload->>'locator_hash'` field,
// where two S3 versions of the same state file MUST be distinguishable so
// the workflow coordinator can plan and dispatch one work item per version.
//
// Do NOT use LocatorHash for the drift resolver join key. The drift join is
// scope-level and version-agnostic — see ScopeLocatorHash and the alignment
// contract documented at go/internal/collector/terraformstate/locator_hash_scope_alignment_test.go
// (issue #203).
func LocatorHash(source StateKey) string {
	sum := sha256.Sum256([]byte(string(source.BackendKind) + "\x00" + source.Locator + "\x00" + source.VersionID))
	return hex.EncodeToString(sum[:])
}

// ScopeLocatorHash returns the version-agnostic durable hash that identifies
// one Terraform state-snapshot scope across both the state and config sides
// of the drift pipeline.
//
// The hash digests only BackendKind and Locator. It MUST stay aligned with
// scope.NewTerraformStateSnapshotScope, which drops VersionID by design — a
// state-snapshot scope groups every observed S3 object version of the same
// state file under one durable identity, with per-version generations
// carrying lineage and serial. The drift resolver join compares the two
// hashes byte-for-byte (go/internal/storage/postgres/tfstate_backend_canonical.go
// against the scope hash parsed at
// go/internal/reducer/terraform_config_state_drift.go); if the formulas
// diverge, every drift intent silently rejects with
// ErrNoConfigRepoOwnsBackend (issue #203).
//
// Use this function — not LocatorHash — when computing the join key for the
// canonical resolver path.
func ScopeLocatorHash(backendKind BackendKind, locator string) string {
	sum := sha256.Sum256([]byte(string(backendKind) + "\x00" + locator))
	return hex.EncodeToString(sum[:])
}

func locatorHash(source StateKey) string {
	return LocatorHash(source)
}
