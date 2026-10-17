// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package terraformstate

// IsLegitimatelyEmptyBody reports whether a parsed Terraform state holds no
// body facts: zero resource, output, and module facts. A legitimately
// emptied state (for example after `terraform destroy` removes every managed
// resource and output) still parses as a JSON object root with matching
// lineage/serial identity and still emits its unconditional snapshot fact,
// so the parse succeeds and yields exactly one envelope with zero body
// facts. That shape is what the tfstate trusted-deletion attestation (#8329)
// gates on: only an empty body plus an explicit operator attestation stamps
// TrustedDeletion onto the generation.
//
// Warning facts (such as state_in_vcs) are collector metadata, not state
// content: they do not affect this check. Failure shapes never reach it: a
// missing state (ErrStateMissing), an over-ceiling state (ErrStateTooLarge),
// and an identity-mismatched state all fail before any parse result exists,
// so there is nothing to attest.
func IsLegitimatelyEmptyBody(resourceFacts, outputFacts, moduleFacts int64) bool {
	return resourceFacts == 0 && outputFacts == 0 && moduleFacts == 0
}
