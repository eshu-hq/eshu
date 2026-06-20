package evidencebundle

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
	if len(bundle.Contents.AnswerPackets) == 0 && len(bundle.Contents.InvestigationPackets) == 0 {
		return fmt.Errorf("bundle must include at least one answer or investigation packet")
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
	text := string(raw)
	switch {
	case privateEndpointPattern.MatchString(text):
		return fmt.Errorf("private endpoint is not allowed in evidence bundle")
	case credentialPattern.MatchString(text):
		return fmt.Errorf("credential canary is not allowed in evidence bundle")
	case rawPromptPattern.MatchString(text):
		return fmt.Errorf("raw prompt or provider response is not allowed in evidence bundle")
	case localPathPattern.MatchString(text):
		return fmt.Errorf("local absolute path is not allowed in evidence bundle")
	}
	return nil
}
