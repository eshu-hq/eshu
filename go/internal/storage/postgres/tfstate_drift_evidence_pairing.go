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

// resourceAddressKey returns the trailing "TYPE.NAME[INDEX]" portion of a
// canonical Terraform resource address (or, for a state-side data source,
// "data.TYPE.NAME[INDEX]" — see the "data. prefix" section below), by
// FRONT-stripping leading "module.<name>[<index>]." pairs rather than taking
// the last two dot-separated segments.
//
// Front-stripping, not end-taking, is required for correctness. A full
// Terraform address is built left-to-right as
// ("module.<name>[<index>]." )* "TYPE.NAME[INDEX]", and EITHER a module
// call's own name (`module.vpc["a.b"].aws_x.y`) OR a for_each instance's
// index key (`aws_route53_record.this["api.example.com"]`) can itself
// contain a literal "." or "]" inside its quoted brackets. This function's
// earlier shape — `strings.Split(address, ".")`, keep the last two segments
// — took the LAST two "." positions regardless of what produced them, so it
// silently truncated mid-index on any dotted key: reviewer-supplied,
// empirically-run proof showed `aws_route53_record.this["api.example.com"]`
// and the UNRELATED `aws_acm_certificate.cert["www.example.com"]` both
// collapsing to the identical wrong key `example.com"]`. Front-stripping
// instead walks the address from the LEFT, consuming and discarding one
// "module.<name>[<index>]." segment at a time (skipModuleNameSegment), so
// bracket/quote content is never mistaken for a "." boundary; whatever is
// left after the loop is returned verbatim, byte-identical to what follows
// the address's own last module prefix.
//
// The "data." prefix (deliberate decision, not an oversight): when present,
// it is preserved as part of the returned key, NOT stripped. Terraform
// itself treats a data source and a managed resource of the same TYPE.NAME
// as entirely different resources, so collapsing "data.aws_ami.ubuntu" and
// "aws_ami.ubuntu" to one key — which the old last-two-segments shape did —
// risks pairing two unrelated resources. In practice this collision can
// only threaten the STATE side of a pairing: PostgresDriftEvidenceLoader's
// config-only rows are always built from the parser's `terraform_resources`
// bucket (tfstate_drift_evidence.go's emitConfigRowsForEntry), and `data`
// blocks are parsed into the SEPARATE `terraform_data_sources` bucket that
// loader never reads, so a config-only row can never carry a
// "data." prefix. The collector DOES emit a "data."-prefixed
// terraform_state_resource fact for a state-side data source
// (internal/collector/terraformstate/identity.go's resourceAddress,
// `if resource.Mode == "data" { prefix += "data." }`), so a state-only row
// legitimately can. Preserving the prefix here — rather than special-casing
// it away — keeps such a row from silently colliding with an unrelated
// managed resource sharing the same type and name; it falls out naturally
// from front-stripping ONLY "module." segments and returning everything
// else verbatim.
//
// Bracket/quote handling: the scan (skipModuleNameSegment,
// hasResourceTypeNameShape) tracks bracket depth and double-quote state, so
// a "." or "]" inside a quoted index string is never mistaken for a segment
// boundary. It does not handle a literal double-quote character escaped
// inside a quoted index key (vanishingly rare in practice — a for_each key
// that itself contains a `"`); such an address either fails the trailing
// structural-validity check (returns false, refusing to pair) or, if it
// happens to parse to some other shape, cannot silently collide with a
// GENUINE resource's key, since real Terraform addresses never contain an
// unescaped `"` outside a quoted index.
//
// Returns ("", false) when the address does not resolve to a well-formed
// TYPE.NAME shape after stripping module prefixes — a malformed address, an
// address that is only a module chain with no trailing resource, or an
// unterminated bracket/quote. Callers MUST treat this as "refuse to pair,"
// never as "pair anyway."
func resourceAddressKey(address string) (string, bool) {
	remaining := address
	for {
		rest, ok := strings.CutPrefix(remaining, "module.")
		if !ok {
			break
		}
		afterName, ok := skipModuleNameSegment(rest)
		if !ok {
			return "", false
		}
		remaining = afterName
	}
	if !hasResourceTypeNameShape(remaining) {
		return "", false
	}
	return remaining, true
}

// skipModuleNameSegment consumes one module-call name — and its optional
// bracketed index, which may itself contain "." or "]" characters inside a
// quoted key — from the front of `rest` and returns whatever follows the
// terminating "." found at bracket depth zero outside a quoted index.
// Returns ("", false) when no such terminator exists: a module name that
// consumes the rest of the address (no trailing resource), or an
// unterminated bracket/quote. Both are malformed shapes this function
// refuses to guess through, per resourceAddressKey's "refuse to pair"
// contract.
func skipModuleNameSegment(rest string) (string, bool) {
	depth := 0
	inQuotes := false
	for i := 0; i < len(rest); i++ {
		switch c := rest[i]; {
		case c == '"':
			inQuotes = !inQuotes
		case inQuotes:
			// Inside a quoted index key: "." and "]" are literal key
			// content, not address structure.
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == '.' && depth == 0:
			return rest[i+1:], true
		}
	}
	return "", false
}

// hasResourceTypeNameShape reports whether `remaining` looks like a
// well-formed "TYPE.NAME[INDEX]" (or "data.TYPE.NAME[INDEX]") tail: at
// least one "." at bracket depth zero outside a quoted index, with
// non-empty content on both sides. It intentionally does not validate
// content beyond that — resourceAddressKey returns `remaining` verbatim
// once this passes, so any prefix before the first depth-zero dot (such as
// "data.") is preserved intact rather than reinterpreted.
func hasResourceTypeNameShape(remaining string) bool {
	depth := 0
	inQuotes := false
	for i := 0; i < len(remaining); i++ {
		switch c := remaining[i]; {
		case c == '"':
			inQuotes = !inQuotes
		case inQuotes:
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == '.' && depth == 0:
			return i > 0 && i < len(remaining)-1
		}
	}
	return false
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
