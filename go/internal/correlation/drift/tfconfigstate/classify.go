package tfconfigstate

// ResourceRow is the normalized config-side, state-side, or prior-state-side
// view of one Terraform resource address. The classifier consumes three of
// them — config, state, prior — and emits a DriftKind or the empty string
// when the three views agree.
//
// All comparisons happen on `Attributes`. Computed/unknown config values must
// be marked in `UnknownAttributes` so the attribute-drift classifier can treat
// them as "no signal" rather than a mismatch against a concrete state value.
type ResourceRow struct {
	// Address is the canonical Terraform resource address — e.g.
	// "module.app.aws_instance.web" — used as the join key across config,
	// state, and prior-state views. The classifier never compares addresses
	// internally; the candidate builder already keys candidates on this value.
	Address string `json:"address"`
	// ResourceType is the Terraform type identifier (e.g. "aws_s3_bucket")
	// used to look up the attribute allowlist.
	ResourceType string `json:"resource_type"`
	// Attributes maps attribute path (e.g. "versioning.enabled") to the
	// stringified value observed on this side. The classifier treats two
	// values as a mismatch only when both sides carry a concrete value and
	// neither marks the attribute as unknown.
	Attributes map[string]string `json:"attributes,omitempty"`
	// UnknownAttributes marks attributes whose config-side value is computed
	// or unresolved (e.g. an HCL local or data reference). The state-side
	// row never sets this; only the config-side row consults it.
	UnknownAttributes map[string]bool `json:"unknown_attributes,omitempty"`
	// LineageRotation flags a prior-state row that belongs to a different
	// lineage than the current state — i.e. a state-file rotation, not a
	// real resource removal. The classifier suppresses removed_from_state
	// when this is true.
	LineageRotation bool `json:"lineage_rotation,omitempty"`
	// PreviouslyDeclaredInConfig marks a state-side row whose address used
	// to appear in a prior config snapshot but no longer does. The
	// classifier requires this signal to emit removed_from_config; raw
	// state-only presence is not enough (it would otherwise collide with
	// added_in_state for genuinely operator-owned resources).
	PreviouslyDeclaredInConfig bool `json:"previously_declared_in_config,omitempty"`
}

// Classify dispatches one resource address through the five drift-kind
// classifiers and returns the matching DriftKind or the empty string when no
// drift fires. The dispatch order is deterministic and the precedence rules
// are designed to suppress false positives:
//
//  1. Lineage-rotation suppression — if a prior row is present and marks
//     LineageRotation, the classifier returns empty regardless of which
//     other classifiers would have fired (rotated state is not real drift).
//  2. removed_from_state — prior row present, current state row absent,
//     config still declares.
//  3. removed_from_config — state row present, config row absent, state row
//     carries the PreviouslyDeclaredInConfig signal. Evaluated BEFORE
//     added_in_state because the previously-declared signal is the
//     strictly stronger evidence.
//  4. added_in_state    — state row present, config row absent. Subsumes
//     the imported-resource case (operator-actionable, not a bug).
//  5. added_in_config   — config row present, state row absent. Subsumes
//     for-each pre-apply key sets (documented).
//  6. attribute_drift   — both sides present and at least one allowlisted
//     attribute differs; computed/unknown config values are skipped.
//
// The dispatch is intentionally exclusive: each call returns at most one
// drift kind. Cross-address cases (e.g. moved blocks producing one
// removed_from_config plus one added_in_config) surface through separate
// classifier calls keyed by address.
func Classify(config, state, prior *ResourceRow) DriftKind {
	if prior != nil && prior.LineageRotation {
		return ""
	}
	if removed := classifyRemovedFromState(config, state, prior); removed != "" {
		return removed
	}
	if removedConfig := classifyRemovedFromConfig(config, state); removedConfig != "" {
		return removedConfig
	}
	if added := classifyAddedInState(config, state); added != "" {
		return added
	}
	if addedConfig := classifyAddedInConfig(config, state); addedConfig != "" {
		return addedConfig
	}
	if attrDrift := classifyAttributeDrift(config, state); attrDrift != "" {
		return attrDrift
	}
	return ""
}

// classifyAddedInState fires when state carries the address and config does
// not. Imported-resource cases land here too — operator-actionable, not a bug.
func classifyAddedInState(config, state *ResourceRow) DriftKind {
	if state == nil {
		return ""
	}
	if config != nil {
		return ""
	}
	return DriftKindAddedInState
}

// classifyAddedInConfig fires when config carries the address and state does
// not. For-each pre-apply key sets land here too — documented as expected
// pre-apply state.
func classifyAddedInConfig(config, state *ResourceRow) DriftKind {
	if config == nil {
		return ""
	}
	if state != nil {
		return ""
	}
	return DriftKindAddedInConfig
}

// classifyRemovedFromState fires when the prior generation carried the address
// and the current state does not, while config still declares it. The
// LineageRotation flag suppresses the classification — a rotated state file is
// not a real removal.
func classifyRemovedFromState(config, state, prior *ResourceRow) DriftKind {
	if prior == nil || config == nil {
		return ""
	}
	if state != nil {
		return ""
	}
	if prior.LineageRotation {
		return ""
	}
	return DriftKindRemovedFromState
}

// classifyRemovedFromConfig fires when current state carries the address and
// the latest config snapshot no longer declares it. The state-side row must
// carry the PreviouslyDeclaredInConfig signal — without it the case is
// indistinguishable from added_in_state for genuinely operator-owned cloud
// resources.
func classifyRemovedFromConfig(config, state *ResourceRow) DriftKind {
	if state == nil {
		return ""
	}
	if config != nil {
		return ""
	}
	if !state.PreviouslyDeclaredInConfig {
		return ""
	}
	return DriftKindRemovedFromConfig
}

// classifyAttributeDrift fires when both sides carry the address and at least
// one allowlisted attribute differs. Attributes marked as unknown on the
// config side are treated as "no signal" — they never raise drift against a
// concrete state value. The allowlist is keyed by ResourceType and consulted
// in deterministic order.
func classifyAttributeDrift(config, state *ResourceRow) DriftKind {
	if config == nil || state == nil {
		return ""
	}
	allowed := AllowlistFor(config.ResourceType)
	if len(allowed) == 0 {
		return ""
	}
	for _, attr := range allowed {
		if config.UnknownAttributes[attr] {
			continue
		}
		cfgValue, cfgHas := config.Attributes[attr]
		stateValue, stateHas := state.Attributes[attr]
		if !cfgHas || !stateHas {
			continue
		}
		if cfgValue != stateValue {
			return DriftKindAttributeDrift
		}
	}
	return ""
}
