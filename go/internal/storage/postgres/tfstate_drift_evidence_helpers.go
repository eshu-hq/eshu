package postgres

import (
	"encoding/json"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
)

// hasStateOnlyAddress reports whether the join has at least one address present
// in state but absent from config. The prior-config walk can only promote
// state-only addresses to PreviouslyDeclaredInConfig=true; when no such
// address exists, the walk's DB round-trip and per-intent log are wasted work.
// Short-circuiting here keeps the loader's hot path cheap on the common case
// where every state resource also appears in config (aligned snapshots, no
// removed_from_config candidates).
//
// Hoisted from tfstate_drift_evidence.go (issue #169) to keep the loader file
// under the CLAUDE.md 500-line cap; behavior is unchanged.
func hasStateOnlyAddress(
	config, state map[string]*tfconfigstate.ResourceRow,
) bool {
	for address := range state {
		if _, inConfig := config[address]; !inConfig {
			return true
		}
	}
	return false
}

// mergeDriftRows unions the address keyspaces of config, state, and prior and
// emits one AddressedRow per address that appears in any source. Aligned
// addresses (config and state both present with identical Attributes, no
// prior signal) pass through; classify.go returns the empty string for them
// and tfconfigstate.BuildCandidates drops them before they reach the engine.
//
// PreviouslyDeclaredInConfig is set to true on state-only addresses present
// in priorConfigAddresses (the set returned by loadPriorConfigAddresses).
// State-only addresses absent from that set keep PreviouslyDeclaredInConfig
// false and surface as added_in_state — the conservative outside-window
// fallback for operator-imported resources that were never in config or
// whose declaration falls outside the PriorConfigDepth window.
//
// Hoisted from tfstate_drift_evidence.go (issue #169) to keep the loader file
// under the CLAUDE.md 500-line cap; behavior is unchanged.
func mergeDriftRows(
	config, state, prior map[string]*tfconfigstate.ResourceRow,
	priorConfigAddresses map[string]struct{},
) []tfconfigstate.AddressedRow {
	addresses := map[string]struct{}{}
	for address := range config {
		addresses[address] = struct{}{}
	}
	for address := range state {
		addresses[address] = struct{}{}
	}
	for address := range prior {
		addresses[address] = struct{}{}
	}
	if len(addresses) == 0 {
		return nil
	}
	out := make([]tfconfigstate.AddressedRow, 0, len(addresses))
	for address := range addresses {
		cfg := config[address]
		st := state[address]
		pr := prior[address]
		resourceType := ""
		switch {
		case cfg != nil:
			resourceType = cfg.ResourceType
		case st != nil:
			resourceType = st.ResourceType
		case pr != nil:
			resourceType = pr.ResourceType
		}
		if cfg == nil && st != nil {
			if _, declared := priorConfigAddresses[address]; declared {
				st.PreviouslyDeclaredInConfig = true
			}
		}
		out = append(out, tfconfigstate.AddressedRow{
			Address:      address,
			ResourceType: resourceType,
			Config:       cfg,
			State:        st,
			Prior:        pr,
		})
	}
	return out
}

// decodeJSONArray decodes a Postgres JSON array column into a slice of
// generic maps. The label is included in the error string so callers can tell
// which logical bucket failed (terraform_resources vs others) without
// inspecting the raw bytes.
//
// Hoisted from tfstate_drift_evidence.go (issue #169); behavior unchanged.
func decodeJSONArray(raw []byte, label string) ([]map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var entries []map[string]any
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode %s array: %w", label, err)
	}
	return entries, nil
}

// coerceJSONString formats one leaf JSON value into the canonical drift-
// comparison string. flattenStateAttributes owns recursion into nested maps
// and singleton-array-wrapped repeated blocks (see
// tfstate_drift_evidence_state_row.go); coerceJSONString operates on the
// leaves it produces and must stay byte-identical to the parser-side
// ctyValueToDriftString output
// (go/internal/parser/hcl/terraform_resource_attributes.go) so the classifier's
// value-equality check at
// go/internal/correlation/drift/tfconfigstate/classify.go:171 fires
// deterministically across both sides.
//
// Encoding contract per leaf type:
//   - nil          → ""
//   - string       → identity
//   - bool         → "true" / "false" (NEVER "cty.True" or other Go-formatted forms)
//   - default      → fmt.Sprint(value)
//
// The default branch covers numeric leaves decoded from JSON as float64.
// fmt.Sprint(float64) renders integers as their decimal form for values below
// ~1e6 but switches to scientific notation ("1e+06", "1e+07", …) at or above
// 1e6. The parser side encodes integers with strconv.FormatInt, which is
// always decimal. The encodings diverge above the 1e6 threshold.
//
// Safe for all v1 allowlist numeric attributes (aws_lambda_function.memory_size
// up to 10240, aws_lambda_function.timeout up to 900, aws_db_instance numeric
// versions). Any new numeric allowlist entry whose real-world values could
// reach 1e6 (e.g. aws_cloudwatch_log_group.retention_in_days at multi-year
// retention, large disk sizes in bytes) needs a regression test AND a
// coordinated change here — switching the default branch to a numeric-aware
// formatter that produces decimal output.
//
// Hoisted from tfstate_drift_evidence.go (issue #169); behavior unchanged.
func coerceJSONString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	default:
		return fmt.Sprint(value)
	}
}
