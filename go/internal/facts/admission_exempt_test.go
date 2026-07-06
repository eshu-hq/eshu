// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facts

import "testing"

// admissionExemptGitKinds are the legacy git code-graph fact kinds registered
// with a payload_schema reference but deliberately kept out of the
// schema-version admission regime. Registering them records their contract
// (payload_schema) in the fact-kind registry without flipping their runtime
// admission behavior, which must stay identical to an unregistered kind
// (CompatibilityUnknownKind). See issue #4752.
var admissionExemptGitKinds = []string{"file", "repository"}

// TestAdmissionExemptKindsAreRegisteredWithPayloadSchema proves the file and
// repository kinds are present in the generated registry and carry the
// checked-in JSON Schema reference S1 generated, while being flagged
// admission-exempt.
func TestAdmissionExemptKindsAreRegisteredWithPayloadSchema(t *testing.T) {
	t.Parallel()

	wantSchema := map[string]string{
		"file":       "sdk/go/factschema/schema/file.v1.schema.json",
		"repository": "sdk/go/factschema/schema/repository.v1.schema.json",
	}
	for _, kind := range admissionExemptGitKinds {
		entry, ok := FactKindRegistryEntryFor(kind)
		if !ok {
			t.Fatalf("FactKindRegistryEntryFor(%q) ok = false, want true (kind must be registered)", kind)
		}
		if !entry.AdmissionExempt {
			t.Fatalf("registry entry for %q AdmissionExempt = false, want true", kind)
		}
		if entry.PayloadSchema != wantSchema[kind] {
			t.Fatalf("registry entry for %q PayloadSchema = %q, want %q", kind, entry.PayloadSchema, wantSchema[kind])
		}
		// An admission-exempt kind must not claim a schema version; its
		// versioned-admission fields stay blank so nothing reads it as
		// version-admitted.
		if entry.SchemaVersion != "" {
			t.Fatalf("registry entry for %q SchemaVersion = %q, want \"\" (exempt kinds are unversioned)", kind, entry.SchemaVersion)
		}
	}
}

// TestAdmissionExemptKindsStayCompatibilityUnknownKind is the load-bearing
// safety assertion: registering file/repository must NOT change their runtime
// admission classification. SchemaVersion must still report the kind as
// core-unowned, and ClassifySchemaVersion must return CompatibilityUnknownKind
// for every candidate version — exactly as it did before the kinds were
// registered. This is what keeps admission (and therefore golden-corpus
// projection) byte-identical.
func TestAdmissionExemptKindsStayCompatibilityUnknownKind(t *testing.T) {
	t.Parallel()

	for _, kind := range admissionExemptGitKinds {
		if version, ok := SchemaVersion(kind); ok {
			t.Fatalf("SchemaVersion(%q) = (%q, true), want ok=false; an exempt kind must not be a versioned core kind", kind, version)
		}
		// The collector emits these envelopes with a blank schema version
		// today, and it must keep passing admission. Also prove a populated
		// candidate classifies as unknown, so no future stamp is silently
		// admitted or rejected.
		for _, candidate := range []string{"", "1.0.0", "2.0.0", "not-a-version"} {
			if got := ClassifySchemaVersion(kind, candidate); got != CompatibilityUnknownKind {
				t.Fatalf("ClassifySchemaVersion(%q, %q) = %q, want %q", kind, candidate, got, CompatibilityUnknownKind)
			}
			if err := ValidateSchemaVersion(kind, candidate); err != nil {
				t.Fatalf("ValidateSchemaVersion(%q, %q) = %v, want nil (unknown kinds pass admission unchanged)", kind, candidate, err)
			}
		}
	}
}

// TestAdmissionExemptKindsAreReservedCoreKinds documents the one intended,
// additive behavior change: the exempt kinds become reserved core fact kinds
// so an out-of-tree component cannot claim the "file"/"repository" names. This
// is a name-reservation change only; it does not touch admission.
func TestAdmissionExemptKindsAreReservedCoreKinds(t *testing.T) {
	t.Parallel()

	for _, kind := range admissionExemptGitKinds {
		if !IsCoreFactKind(kind) {
			t.Fatalf("IsCoreFactKind(%q) = false, want true (registered kinds are core-reserved)", kind)
		}
	}
}
