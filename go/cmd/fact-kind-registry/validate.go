// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// validatePayloadSchemaReference fails closed when a non-blank payload_schema
// value is not a real file contained in sdk/go/factschema/schema/. It rejects,
// in order: a non-clean ref (anything that is not equal to its slash-cleaned
// form, which kills "..", "." and trailing-slash segments), a ref outside the
// schema directory, a resolved path that escapes the schema directory even
// after cleaning, a missing file, and a directory. A dangling or escaping
// reference — a typo, a moved file, a schema that was never generated, or a
// traversal that points at another repo file — must never be accepted as a
// valid contract pointer.
func validatePayloadSchemaReference(repoRoot, family, kind, payloadSchema string) error {
	ref := strings.TrimSpace(payloadSchema)
	if ref == "" {
		return nil
	}
	// The committed ref must already be in clean, forward-slash form. Rejecting
	// non-clean refs up front removes "..", "." and trailing slashes before any
	// filesystem resolution, so containment cannot be bypassed by traversal.
	if ref != path.Clean(ref) {
		return fmt.Errorf("family %q kind %q payload_schema %q is not a clean path (no ., .., or trailing slash); use %s", family, kind, ref, path.Clean(ref))
	}
	wantPrefix := payloadSchemaDir + "/"
	if !strings.HasPrefix(ref, wantPrefix) {
		return fmt.Errorf("family %q kind %q payload_schema %q must be under %s", family, kind, ref, payloadSchemaDir)
	}
	// Defense in depth: confirm the resolved absolute path is still contained
	// in the schema directory. filepath.Rel returning a "../"-prefixed or
	// absolute path means the ref escaped.
	schemaDirAbs := filepath.Join(repoRoot, filepath.FromSlash(payloadSchemaDir))
	abs := filepath.Join(repoRoot, filepath.FromSlash(ref))
	rel, err := filepath.Rel(schemaDirAbs, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("family %q kind %q payload_schema %q resolves outside %s", family, kind, ref, payloadSchemaDir)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("family %q kind %q payload_schema %q does not exist: %w", family, kind, ref, err)
	}
	if info.IsDir() {
		return fmt.Errorf("family %q kind %q payload_schema %q is a directory, want a file", family, kind, ref)
	}
	return nil
}

// validateLifecycleMarker fails closed when a non-blank deprecated_in or
// removed_in value is not a canonical MAJOR.MINOR.PATCH semver. These markers
// feed conformance and schema-diff tooling that compares them as versions, so a
// typo like "next" or "2" must never reach the generated source-of-truth
// artifact. It reuses facts.IsCanonicalSchemaVersion so the generator and the
// runtime classifier share one definition of a well-formed version.
func validateLifecycleMarker(family, kind, field, value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	if !facts.IsCanonicalSchemaVersion(v) {
		return fmt.Errorf("family %q kind %q %s %q is not a canonical semver (MAJOR.MINOR.PATCH)", family, kind, field, v)
	}
	return nil
}

// validateFamilyMetadata rejects a family whose required contract fields are
// blank or whose truth profile does not satisfy its extra constraint
// (deterministic requires provider-key independence, optional_semantic requires
// a policy gate).
func validateFamilyMetadata(name string, spec familySpec) error {
	for field, value := range map[string]string{
		"lifecycle_owner": spec.LifecycleOwner,
		"schema_version":  spec.SchemaVersion,
		"reducer_domain":  spec.ReducerDomain,
		"projection_hook": spec.ProjectionHook,
		"admission_hook":  spec.AdmissionHook,
		"read_surface":    spec.ReadSurface,
		"truth_profile":   spec.TruthProfile,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("family %q missing %s", name, field)
		}
	}
	switch facts.FactKindTruthProfile(spec.TruthProfile) {
	case facts.FactKindTruthDeterministic:
		if !spec.ProviderKeyIndependent {
			return fmt.Errorf("family %q deterministic truth requires provider_key_independent", name)
		}
	case facts.FactKindTruthProviderGated, facts.FactKindTruthFixtureGated:
	case facts.FactKindTruthOptionalSemantic:
		if strings.TrimSpace(spec.PolicyGate) == "" {
			return fmt.Errorf("family %q optional_semantic truth requires policy_gate", name)
		}
	default:
		return fmt.Errorf("family %q unsupported truth_profile %q", name, spec.TruthProfile)
	}
	return nil
}

// validateKindOverrides rejects a per-kind override map whose value is blank or
// whose key is not one of the family's declared kinds, so a stale override
// cannot silently outlive the kind it referenced.
func validateKindOverrides(name, field string, overrides map[string]string, specKinds []string) error {
	if len(overrides) == 0 {
		return nil
	}
	known := make(map[string]struct{}, len(specKinds))
	for _, kind := range specKinds {
		known[kind] = struct{}{}
	}
	for kind, value := range overrides {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("family %q %s for kind %q is blank", name, field, kind)
		}
		if _, ok := known[kind]; !ok {
			return fmt.Errorf("family %q %s references unknown kind %q", name, field, kind)
		}
	}
	return nil
}
