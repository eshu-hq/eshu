// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reportbundle

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/sdk/go/collector"
)

// ValidateOptions controls the strictness Validate applies. RequirePublic
// mirrors `eshu report validate --require-public`: it rejects any bundle whose
// Redaction.Profile is not ProfilePublic, which is what the wrong-answer issue
// template instructs maintainers to run before accepting an attachment.
type ValidateOptions struct {
	RequirePublic bool
}

// Validate checks a finished Bundle: schema_version fail-closed (mirroring
// evidencebundle/validate.go:14-15), a required bundle_id, profile↔payloads
// consistency, the --require-public posture when requested, and the share-safe
// key-name gate — collector.ValidateShareSafeKeys applied to the whole bundle
// document except the PayloadAttachment section, which is the one place a
// private-triage bundle is allowed to carry raw excerpt/fact bytes under
// --include-payloads. A public-profile bundle that trips the gate is a bug:
// Capture calls Validate before returning, so `eshu report capture` refuses to
// write such a bundle rather than silently emitting it.
//
// The Payloads section is stripped from the share-safe walk ONLY for a
// private-triage bundle, where it is legitimately present. A public-profile
// bundle carrying a non-nil Payloads section is a redaction-safety violation
// (raw excerpt/fact bytes under a "share-safe" label) and is REJECTED rather
// than stripped-then-passed — otherwise `eshu report validate --require-public`
// would green-light a bundle that leaks private payload bytes.
func Validate(bundle Bundle, opts ValidateOptions) error {
	if bundle.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema_version: got %q want %q", bundle.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(bundle.BundleID) == "" {
		return fmt.Errorf("bundle_id is required")
	}
	profile := bundle.Redaction.Profile
	switch profile {
	case ProfilePublic:
		// A share-safe bundle must not carry any raw payload attachment.
		if bundle.Payloads != nil {
			return fmt.Errorf("public redaction profile must not carry a payloads attachment: a payload section is only valid under the %q profile", ProfilePrivateTriage)
		}
	case ProfilePrivateTriage:
		// The profile exists solely to carry payloads; a private-triage
		// bundle with no attachment is mislabeled.
		if bundle.Payloads == nil {
			return fmt.Errorf("%q redaction profile requires a payloads attachment; a bundle with no payloads must use the %q profile", ProfilePrivateTriage, ProfilePublic)
		}
	default:
		return fmt.Errorf("redaction profile %q is unsupported", profile)
	}
	if opts.RequirePublic && profile != ProfilePublic {
		return fmt.Errorf("bundle redaction profile %q fails --require-public: only %q bundles may be treated as share-safe", profile, ProfilePublic)
	}

	// Exclude the (now consistency-checked) PayloadAttachment from the
	// share-safe walk so a legitimate private-triage attachment cannot trip
	// its own bundle's gate. Every other field is still walked. For a public
	// bundle Payloads is guaranteed nil by the check above, so nothing is
	// hidden from the walk.
	checkable := bundle
	checkable.Payloads = nil
	raw, err := json.Marshal(checkable)
	if err != nil {
		return fmt.Errorf("marshal bundle for validation: %w", err)
	}
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("decode bundle for validation: %w", err)
	}
	if err := collector.ValidateShareSafeKeys(doc); err != nil {
		return fmt.Errorf("share-safe gate: %w", err)
	}
	return nil
}
