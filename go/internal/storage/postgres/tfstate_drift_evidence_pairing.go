// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Config/state spurious-mismatch pairing for PostgresDriftEvidenceLoader
// (issue #5572 follow-up).
//
// tfstate_drift_evidence_module_confidence.go already flags a config-side
// ResourceRow low-confidence when its address depended on an unresolved
// module-prefix fallback. That alone is only half the fix. An unresolved
// module prefix does not just make ONE address uncertain — it makes the
// config/state JOIN KEY wrong, which produces a spurious MISMATCH PAIR:
//
//   - the config-side row lands at the fallback address (root-module shape,
//     or a masked, too-shallow prefix for the depth_exceeded case), and
//   - the state-side row for the SAME real resource carries Terraform's own,
//     correct, prefixed address.
//
// Because the two addresses differ, mergeDriftRows's address-keyed union
// never joins them: it emits a config-only "added_in_config" candidate at
// the fallback address AND a state-only "added_in_state" candidate at the
// real address. Only the config-only half carried ModuleResolutionReason
// before this file existed, so an operator querying outcome=exact still saw
// the state-only half of a finding this feature exists specifically to flag
// as uncertain.
//
// pairSpuriousModuleMismatches closes that gap by mirroring
// ModuleResolutionReason onto the paired state-only row, but only when the
// pairing is unambiguous. See resourceAddressKey and
// pairSpuriousModuleMismatches's own doc comments for why a blind
// <type>.<name> match is not safe to apply universally: Terraform's own
// singleton-resource module convention ("aws_s3_bucket.this",
// "aws_iam_role.this", …) means the same resource key legitimately recurs
// across unrelated modules, so this file refuses to guess whenever more than
// one candidate shares a key on either side.
package postgres

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
)

// resourceAddressKey returns the trailing "<type>.<name>[index]" segment of a
// canonical Terraform resource address, stripping any number of leading
// "module.<name>." pairs. Terraform's address grammar guarantees this is
// always the last two dot-separated segments: a full address is built as
// zero or more "module.<name>." pairs followed by exactly "<type>.<name>"
// (with any count/for_each index suffix attached directly to the name
// segment, no separating dot for the common index shapes this repo's
// allowlisted resource types use). configRowFromParserEntry constructs
// addresses the same way (`resourceType + "." + resourceName`, optionally
// module-prefixed), so splitting on "." and taking the last two segments
// recovers exactly what that builder started from — no module-prefix map
// lookup needed at pairing time.
//
// Returns ("", false) for a malformed address with fewer than two segments
// (should not happen for any address this package's own row builders emit;
// guards against a future caller passing something else).
func resourceAddressKey(address string) (string, bool) {
	segments := strings.Split(address, ".")
	if len(segments) < 2 {
		return "", false
	}
	return strings.Join(segments[len(segments)-2:], "."), true
}

// pairSpuriousModuleMismatches mirrors ModuleResolutionReason from a
// config-only row onto its paired state-only row when an unresolved
// module-prefix chain produced a spurious added_in_config/added_in_state
// mismatch pair (see this file's package doc comment). Mutates the
// `state` map's ResourceRow values in place; `config` is read-only.
//
// A pairing is applied ONLY when it is unambiguous: exactly one
// low-confidence config-only row and exactly one state-only row share the
// same resourceAddressKey. "Config-only" and "state-only" here means the
// row's address is absent from the OTHER map entirely — i.e. exactly the
// membership test classify.go's classifyAddedInConfig/classifyAddedInState
// already require, so this function only ever touches rows that were going
// to become an added_in_config/added_in_state pair regardless.
//
// The unambiguous-only restriction bounds the false-pairing risk of matching
// on resource type+name alone: Terraform's own idiomatic "singleton
// resource" naming convention (terraform-aws-modules's "aws_s3_bucket.this",
// "aws_iam_role.this", and similar) means the same <type>.<name> key
// routinely recurs across unrelated, independently-resolved modules. Pairing
// on a 1:2+ or 2+:1 collision would risk mirroring the reason onto a
// genuinely unrelated resource that merely shares a name — worse than
// leaving the known half-fixed gap, because it would manufacture a false
// "derived" downgrade on a real, independent finding. Refusing ambiguous
// collisions accepts a narrower, explicitly-scoped miss (the loader will not
// pair when a repo happens to have both an unresolved module AND an
// unrelated genuinely-added resource sharing the exact same key in the same
// generation) in exchange for zero false pairings.
func pairSpuriousModuleMismatches(
	config, state map[string]*tfconfigstate.ResourceRow,
) {
	configOnlyByKey := map[string][]string{}
	for address, row := range config {
		if row.ModuleResolutionReason == "" {
			continue
		}
		if _, inState := state[address]; inState {
			continue
		}
		key, ok := resourceAddressKey(address)
		if !ok {
			continue
		}
		configOnlyByKey[key] = append(configOnlyByKey[key], address)
	}
	if len(configOnlyByKey) == 0 {
		return
	}

	stateOnlyByKey := map[string][]string{}
	for address := range state {
		if _, inConfig := config[address]; inConfig {
			continue
		}
		key, ok := resourceAddressKey(address)
		if !ok {
			continue
		}
		stateOnlyByKey[key] = append(stateOnlyByKey[key], address)
	}

	for key, configAddresses := range configOnlyByKey {
		if len(configAddresses) != 1 {
			continue
		}
		stateAddresses := stateOnlyByKey[key]
		if len(stateAddresses) != 1 {
			continue
		}
		reason := config[configAddresses[0]].ModuleResolutionReason
		state[stateAddresses[0]].ModuleResolutionReason = reason
	}
}
