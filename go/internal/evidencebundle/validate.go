// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidencebundle

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

const (
	// unvalidatedStatus is the validation status both builders emit. A bundle
	// carries it until StampValidation records a real passing Validate run.
	unvalidatedStatus = "unvalidated"
	// validatedStatus marks a bundle whose embedded checks actually ran green.
	validatedStatus = "passed"
)

// StampValidation records that Validate ran green on this bundle and returns
// the stamped copy, with bundle_id recomputed over the new content.
//
// It must be given a bundle as a builder returned it: the id is recomputed over
// the current content, so stamping a hand-modified bundle yields an id that
// RenderJSON's output will not reproduce.
//
// Export paths call it after a nil Validate. Keeping the stamp out of the
// builders is the point: BuildDemoBundle and BuildLiveBundle are pure and run
// no checks, so a bundle they returned pre-stamped would assert a validation
// that never happened for every caller that skipped Validate.
func StampValidation(bundle Bundle) Bundle {
	bundle.Validation.Status = validatedStatus
	bundle.BundleID = bundleID(bundle)
	return bundle
}

// Validate verifies schema, bounds, redaction posture, and private-data canaries.
func Validate(bundle Bundle) error {
	if bundle.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schema: got %q want %q", bundle.SchemaVersion, SchemaVersion)
	}
	if strings.TrimSpace(bundle.Identity.ScopeID) == "" {
		return fmt.Errorf("identity scope_id is required")
	}
	if strings.TrimSpace(bundle.Redaction.Profile) == "" {
		return fmt.Errorf("redaction profile is required")
	}
	if len(bundle.Contents.AnswerPackets) == 0 &&
		len(bundle.Contents.InvestigationPackets) == 0 &&
		bundle.Contents.PipelineState == nil &&
		bundle.Contents.SemanticProviderState == nil {
		return fmt.Errorf("bundle must include at least one answer packet, investigation packet, pipeline state, or semantic provider state")
	}
	if len(bundle.Reproduce) == 0 {
		return fmt.Errorf("bundle must include reproduce calls")
	}
	if err := validateBounds(bundle); err != nil {
		return err
	}
	if err := validateReproduce(bundle.Reproduce); err != nil {
		return err
	}
	if err := validatePrivateCanaries(bundle); err != nil {
		return err
	}
	// Deliberately NOT checked here: that bundle_id matches a rehash of the
	// content. It reads like a cheap integrity check and is worse than nothing.
	// The hash covers the current struct, so every bundle an older binary
	// exported rehashes differently the moment a field is added within v1, and
	// `eshu evidence bundle validate --from` would reject artifacts that were
	// valid when they were written -- which is exactly the artifact's job.
	// It would not buy provenance either: whoever edits a body can rehash it.
	return nil
}

func validateBounds(bundle Bundle) error {
	if bundle.Bounds.MaxAnswerPackets <= 0 || bundle.Bounds.MaxInvestigationPackets <= 0 || bundle.Bounds.MaxHandles <= 0 {
		return fmt.Errorf("bounds must declare positive caps")
	}
	if len(bundle.Contents.AnswerPackets) > bundle.Bounds.MaxAnswerPackets {
		return fmt.Errorf("answer packet count exceeds bundle cap")
	}
	if len(bundle.Contents.InvestigationPackets) > bundle.Bounds.MaxInvestigationPackets {
		return fmt.Errorf("investigation packet count exceeds bundle cap")
	}
	handleCount := 0
	for _, packet := range append(append([]PacketSummary{}, bundle.Contents.AnswerPackets...), bundle.Contents.InvestigationPackets...) {
		handleCount += len(packet.EvidenceHandles)
	}
	handleCount += len(bundle.Contents.CapabilityCatalog.Handles)
	handleCount += len(bundle.Contents.SurfaceInventory.Handles)
	if handleCount > bundle.Bounds.MaxHandles {
		return fmt.Errorf("evidence handle count exceeds bundle cap")
	}
	return nil
}

func validateReproduce(calls []ReproduceCall) error {
	for _, call := range calls {
		switch call.Kind {
		case "api", "cli", "mcp":
		default:
			return fmt.Errorf("unsupported reproduce call kind %q", call.Kind)
		}
		if strings.TrimSpace(call.Target) == "" {
			return fmt.Errorf("reproduce call target is required")
		}
	}
	return nil
}

func validatePrivateCanaries(bundle Bundle) error {
	raw, err := json.Marshal(bundle)
	if err != nil {
		return fmt.Errorf("marshal bundle for validation: %w", err)
	}
	// The shared hosted-governance registry owns this repository's canonical
	// sensitive-shape taxonomy. Check it first: the four package-local patterns
	// below were written against endpoint/credential/prompt/path shapes and
	// match none of the registry's canaries, so relying on them alone let a
	// credential-bearing URL or a raw token through (#4045).
	if err := redact.HostedGovernanceRegistry().
		AssertNoForbiddenCanary(redact.SurfaceOnboardingArtifacts, raw); err != nil {
		return fmt.Errorf("hosted-governance canary is not allowed in evidence bundle: %w", err)
	}
	text := string(raw)
	switch {
	case privateEndpointPattern.MatchString(text),
		privateHostPortPattern.MatchString(text),
		privateAddressPattern.MatchString(text):
		return fmt.Errorf("private endpoint is not allowed in evidence bundle")
	case credentialURLPattern.MatchString(text):
		return fmt.Errorf("credential-bearing URL is not allowed in evidence bundle")
	case credentialPattern.MatchString(text):
		return fmt.Errorf("credential canary is not allowed in evidence bundle")
	case rawPromptPattern.MatchString(text):
		return fmt.Errorf("raw prompt or provider response is not allowed in evidence bundle")
	case localPathPattern.MatchString(text):
		return fmt.Errorf("local absolute path is not allowed in evidence bundle")
	}
	return nil
}
