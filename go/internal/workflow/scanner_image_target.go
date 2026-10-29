// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// ScannerAuthorizationNamespace separates local targets from hosted tenants.
type ScannerAuthorizationNamespace string

const (
	// ScannerAuthorizationLocal identifies an explicitly local scan target.
	ScannerAuthorizationLocal ScannerAuthorizationNamespace = "local"
	// ScannerAuthorizationHosted identifies a tenant-scoped scan target.
	ScannerAuthorizationHosted ScannerAuthorizationNamespace = "hosted"
)

// ScannerRuntimeDigestKind identifies what the runtime's immutable digest names.
type ScannerRuntimeDigestKind string

const (
	// ScannerRuntimeDigestManifest means the runtime digest identifies a manifest.
	ScannerRuntimeDigestManifest ScannerRuntimeDigestKind = "manifest"
	// ScannerRuntimeDigestConfig means the runtime digest identifies image config.
	ScannerRuntimeDigestConfig ScannerRuntimeDigestKind = "config"
)

// ScannerImageTarget is immutable, nonsecret work admitted for one image scan.
// AuthorizationDomain partitions deduplication; it never contains credentials.
type ScannerImageTarget struct {
	WorkItemID             string
	ScanTargetID           string
	AuthorizationNamespace ScannerAuthorizationNamespace
	TenantID               string
	WorkspaceID            string
	SubjectClass           string
	PolicyRevisionHash     string
	AuthorizationDomain    string
	ManifestDigest         string
	Sources                []ScannerImageSourceBinding
	// RegistryDomainConfig is current nonsecret authorization config returned by lookup; it is not persisted.
	RegistryDomainConfig json.RawMessage `json:"-"`
}

// ScannerImageSourceBinding records one authorized source of a derived scan.
// A scan target can have several sources without creating duplicate work.
type ScannerImageSourceBinding struct {
	// SourceObservationID is the DB-owned canonical typed source. SourceFactID
	// names an optional projection and never grants scanner authority.
	SourceObservationID       int64
	SourceFactID              string
	SourceCollectorInstanceID string
	SourceScopeID             string
	SourceGenerationID        string
	Registry                  string
	Repository                string
	RuntimeDigest             string
	RuntimeDigestKind         ScannerRuntimeDigestKind
	RuntimeImageReference     string
	ManifestVerificationID    string
}

// ScannerImageTargetScopeID derives a stable, boundary-partitioned scan scope.
// Policy revision and source facts are attempt/binding identities, not target identity.
func ScannerImageTargetScopeID(target ScannerImageTarget) string {
	var input strings.Builder
	for _, part := range [...]string{
		"scanner-image-target-v1", string(target.AuthorizationNamespace),
		target.TenantID, target.WorkspaceID, target.SubjectClass,
		target.AuthorizationDomain, target.ManifestDigest,
	} {
		fmt.Fprintf(&input, "%d:%s", len(part), part)
	}
	sum := sha256.Sum256([]byte(input.String()))
	return "image://derived/" + hex.EncodeToString(sum[:])
}

// Validate rejects targets that cannot be tied to an immutable image and source.
func (target ScannerImageTarget) Validate() error {
	switch target.AuthorizationNamespace {
	case ScannerAuthorizationLocal:
		if target.TenantID != "" || target.WorkspaceID != "" || target.SubjectClass != "" || target.PolicyRevisionHash != "" {
			return fmt.Errorf("local scanner image target cannot carry a hosted boundary")
		}
	case ScannerAuthorizationHosted:
		if target.TenantID == "" || target.WorkspaceID == "" || target.SubjectClass == "" || target.PolicyRevisionHash == "" {
			return fmt.Errorf("hosted scanner image target requires a complete boundary")
		}
	default:
		return fmt.Errorf("scanner image target authorization namespace %q is unsupported", target.AuthorizationNamespace)
	}
	for name, value := range map[string]string{
		"work_item_id":         target.WorkItemID,
		"authorization_domain": target.AuthorizationDomain,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("scanner image target %s is required", name)
		}
	}
	if err := validateScannerSHA256(target.ManifestDigest); err != nil {
		return fmt.Errorf("scanner image target manifest digest: %w", err)
	}
	if target.ScanTargetID != ScannerImageTargetScopeID(target) {
		return fmt.Errorf("scanner image target scan scope does not match its authorization boundary")
	}
	if len(target.Sources) == 0 {
		return fmt.Errorf("scanner image target requires a source binding")
	}
	for _, source := range target.Sources {
		if err := source.validate(target.ManifestDigest); err != nil {
			return err
		}
	}
	return nil
}

func (source ScannerImageSourceBinding) validate(manifestDigest string) error {
	for name, value := range map[string]string{
		"registry": source.Registry, "repository": source.Repository,
		"source_collector_instance_id": source.SourceCollectorInstanceID,
		"source_scope_id":              source.SourceScopeID,
		"source_generation_id":         source.SourceGenerationID,
		"runtime_image_reference":      source.RuntimeImageReference,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("scanner image source %s is required", name)
		}
	}
	if source.SourceObservationID <= 0 {
		return fmt.Errorf("scanner image source requires a canonical observation")
	}
	if strings.ContainsAny(source.Registry+source.Repository, "@?#") || strings.Contains(source.Registry, "/") {
		return fmt.Errorf("scanner image source registry or repository is not a canonical locator")
	}
	fullRepository := source.Registry + "/" + source.Repository
	if !strings.HasPrefix(source.RuntimeImageReference, fullRepository+":") &&
		!strings.HasPrefix(source.RuntimeImageReference, fullRepository+"@") {
		return fmt.Errorf("scanner image source runtime reference does not match its repository")
	}
	if err := validateScannerSHA256(source.RuntimeDigest); err != nil {
		return fmt.Errorf("scanner image source runtime digest: %w", err)
	}
	switch source.RuntimeDigestKind {
	case ScannerRuntimeDigestManifest:
		if source.RuntimeDigest != manifestDigest {
			return fmt.Errorf("scanner image source runtime manifest digest differs from manifest digest")
		}
	case ScannerRuntimeDigestConfig:
		if source.ManifestVerificationID == "" {
			return fmt.Errorf("scanner image config digest requires protected manifest verification")
		}
	default:
		return fmt.Errorf("scanner image source runtime digest kind %q is unsupported", source.RuntimeDigestKind)
	}
	return nil
}

func validateScannerSHA256(value string) error {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return fmt.Errorf("expected sha256 digest")
	}
	if value != strings.ToLower(value) {
		return fmt.Errorf("expected lowercase sha256 digest")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:")); err != nil {
		return fmt.Errorf("invalid sha256 digest: %w", err)
	}
	return nil
}
