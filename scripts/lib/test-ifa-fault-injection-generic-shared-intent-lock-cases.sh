#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic proof for scripts/lib/ifa_fault_generic_shared_intent_lock.sh's
# _ifa_generic_require_intent_writer -- the MANDATORY PRECONDITION ASSERT for
# the shared_intent_lock generic blocker mechanism.
#
# This module exists because of the asymmetry it removes. The table_lock
# mechanism, which is UNEXERCISED against a live stack, already had a 152-line
# hermetic cases module; shared_intent_lock, the ONE mechanism this change
# actually wires to live consumers (code_calls and rationale_edges), had none.
# The only thing naming its precondition anywhere in the mirror was a COMMENT
# in test-verify-ifa-fault-injection.sh calling it "the mandatory non-vacuity
# precondition this change rests on" -- and require_generic_cells greps
# ifa_fault_generic_cells.sh, not this mechanism's own file, so deleting the
# precondition call outright kept the whole mirror green. It also kept the LIVE
# gate green, because both current consumers really do declare an IntentWriter,
# so the guard never fires today; the next family registered
# shared_intent_lock+generic on an EdgeWriter-only handler would have gotten
# exactly the vacuous cell this mechanism was written to prevent.
#
# Sourced by test-verify-ifa-fault-injection.sh; the parent owns strict mode,
# fail(), repo_root, and the ${generic_shared_intent_lock_lib} path variable.

# run_ifa_fault_injection_generic_shared_intent_lock_cases drives every branch
# of the precondition's fail-closed contract, then asserts the wiring the
# branches alone cannot prove: that the wrapper calls the precondition BEFORE
# the shared kill-worker body.
run_ifa_fault_injection_generic_shared_intent_lock_cases() {
	test_ifa_generic_intent_writer_accessor_failure_is_a_failure
	test_ifa_generic_intent_writer_empty_handler_file_fails
	test_ifa_generic_intent_writer_missing_handler_file_fails
	test_ifa_generic_intent_writer_absent_intent_writer_fails
	test_ifa_generic_intent_writer_mention_without_field_fails
	test_ifa_generic_intent_writer_declared_intent_writer_passes
	test_ifa_generic_intent_writer_precondition_is_wired_before_the_body
}

# A registry accessor that fails is NOT "no IntentWriter" and must not be read
# as either verdict: the precondition propagates the failure instead of
# continuing with an empty handler path.
test_ifa_generic_intent_writer_accessor_failure_is_a_failure() (
	# shellcheck source=scripts/lib/ifa_fault_generic_shared_intent_lock.sh
	source "${generic_shared_intent_lock_lib}"
	local rc=0

	ifa_family_handler_go_file() { return 1; }

	_ifa_generic_require_intent_writer testfamily >/dev/null 2>&1 || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "intent-writer precondition did not fail when the registry accessor failed (rc=${rc})"
)

# An empty registered path is a missing row, not a passing family.
test_ifa_generic_intent_writer_empty_handler_file_fails() (
	# shellcheck source=scripts/lib/ifa_fault_generic_shared_intent_lock.sh
	source "${generic_shared_intent_lock_lib}"
	local rc=0 output

	ifa_family_handler_go_file() { printf '\n'; }

	output="$(_ifa_generic_require_intent_writer testfamily 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "intent-writer precondition accepted an empty handler_go_file (rc=${rc})"
	[[ "${output}" == *"no handler_go_file registered"* ]] \
		|| fail "intent-writer precondition did not name the missing handler_go_file row"
)

# A registered path that does not exist is a stale row -- report it as such
# rather than letting rg's own failure masquerade as "declares no IntentWriter".
test_ifa_generic_intent_writer_missing_handler_file_fails() (
	# shellcheck source=scripts/lib/ifa_fault_generic_shared_intent_lock.sh
	source "${generic_shared_intent_lock_lib}"
	local rc=0 output

	ifa_family_handler_go_file() { printf 'go/internal/reducer/no_such_handler_file.go\n'; }

	output="$(_ifa_generic_require_intent_writer testfamily 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "intent-writer precondition accepted a handler_go_file that does not exist (rc=${rc})"
	[[ "${output}" == *"does not exist"* ]] \
		|| fail "intent-writer precondition did not distinguish a missing file from a missing IntentWriter"
)

# The defect the mechanism exists to catch: a real handler file that declares
# no IntentWriter, so a lock on shared_projection_intents cannot engage.
test_ifa_generic_intent_writer_absent_intent_writer_fails() (
	# shellcheck source=scripts/lib/ifa_fault_generic_shared_intent_lock.sh
	source "${generic_shared_intent_lock_lib}"
	local rc=0 output tmp_dir handler
	tmp_dir="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '${tmp_dir}'" EXIT
	handler="${tmp_dir}/edge_writer_only_handler.go"
	printf 'package reducer\n\ntype OnlyEdgeWriterHandler struct {\n\tEdgeWriter CanonicalEdgeWriter\n}\n' >"${handler}"

	ifa_family_handler_go_file() { printf '%s\n' "${handler}"; }

	output="$(_ifa_generic_require_intent_writer testfamily 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "intent-writer precondition passed a handler that declares no IntentWriter (rc=${rc})"
	[[ "${output}" == *"declares no IntentWriter"* ]] \
		|| fail "intent-writer precondition did not name the absent IntentWriter"
)

# The word "IntentWriter" appearing in the file is NOT the field. Every real
# handler here declares its writer interface in the same file as the struct, so
# a substring match over the file is satisfied by a handler that has no field at
# all -- which is what the earlier implementation did, and it passed this exact
# fixture with rc=0 and "precondition confirmed". This is the case that pins the
# difference; without it the guard can be loosened back to a substring match and
# every other case here stays green.
test_ifa_generic_intent_writer_mention_without_field_fails() (
	# shellcheck source=scripts/lib/ifa_fault_generic_shared_intent_lock.sh
	source "${generic_shared_intent_lock_lib}"
	local rc=0 output tmp_dir handler
	tmp_dir="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '${tmp_dir}'" EXIT
	handler="${tmp_dir}/mentions_but_does_not_declare.go"
	printf 'package reducer\n\n// SomeIntentWriter persists rows; this handler does NOT hold one.\ntype SomeIntentWriter interface{ Upsert() error }\n\ntype MentionOnlyHandler struct {\n\tEdgeWriter CanonicalEdgeWriter\n}\n' >"${handler}"

	ifa_family_handler_go_file() { printf '%s\n' "${handler}"; }

	output="$(_ifa_generic_require_intent_writer testfamily 2>&1)" || rc=$?
	[[ "${rc}" -eq 1 ]] \
		|| fail "intent-writer precondition passed a handler that only MENTIONS IntentWriter (interface decl + comment) without holding the field (rc=${rc})"
	[[ "${output}" == *"declares no IntentWriter"* ]] \
		|| fail "intent-writer precondition did not name the absent IntentWriter field for a mention-only handler"
)

# The pass case has to be proven too, or a precondition that always fails would
# satisfy every case above while making the mechanism unusable.
test_ifa_generic_intent_writer_declared_intent_writer_passes() (
	# shellcheck source=scripts/lib/ifa_fault_generic_shared_intent_lock.sh
	source "${generic_shared_intent_lock_lib}"
	local rc=0 output tmp_dir handler
	tmp_dir="$(mktemp -d)"
	# shellcheck disable=SC2064
	trap "rm -rf '${tmp_dir}'" EXIT
	handler="${tmp_dir}/intent_writer_handler.go"
	printf 'package reducer\n\ntype RealHandler struct {\n\tIntentWriter SomeIntentWriter\n}\n' >"${handler}"

	ifa_family_handler_go_file() { printf '%s\n' "${handler}"; }

	output="$(_ifa_generic_require_intent_writer testfamily 2>&1)" || rc=$?
	[[ "${rc}" -eq 0 ]] \
		|| fail "intent-writer precondition rejected a handler that declares an IntentWriter (rc=${rc})"
	[[ "${output}" == *"precondition confirmed"* ]] \
		|| fail "intent-writer precondition passed without saying what it confirmed"
)

# The branches above prove the precondition BEHAVES correctly when called.
# They cannot prove it IS called -- deleting the call from the wrapper leaves
# every one of them green. This case closes that: the wrapper must invoke the
# precondition, and must do so before _ifa_generic_cell_killworker_body, since
# a precondition that runs after the lock is attempted has already spent the
# Compose cycle it exists to save.
test_ifa_generic_intent_writer_precondition_is_wired_before_the_body() {
	local wrapper precondition_line body_line
	wrapper="$(sed -n '/^_ifa_generic_cell_killworker_shared_intent_lock() {$/,/^}$/p' \
		"${generic_shared_intent_lock_lib}")"
	[[ -n "${wrapper}" ]] \
		|| fail "could not find _ifa_generic_cell_killworker_shared_intent_lock in ${generic_shared_intent_lock_lib##*/}"

	# `|| true` is load-bearing, not defensive noise: when the call is absent rg
	# exits 1, and under the parent's `set -e` a bare command substitution
	# assignment aborts the whole mirror silently -- exit 1 with no message,
	# which is the wrong way for this case to fail. Swallowing rg's status here
	# lets the emptiness check below report WHICH guard went missing.
	precondition_line="$(printf '%s\n' "${wrapper}" \
		| rg --line-number --fixed-strings --max-count 1 -- '_ifa_generic_require_intent_writer' \
		| cut -d: -f1 || true)"
	[[ -n "${precondition_line}" ]] \
		|| fail "the shared_intent_lock kill-worker wrapper never calls _ifa_generic_require_intent_writer; the mechanism's non-vacuity precondition is not wired"

	body_line="$(printf '%s\n' "${wrapper}" \
		| rg --line-number --fixed-strings --max-count 1 -- '_ifa_generic_cell_killworker_body' \
		| cut -d: -f1 || true)"
	[[ -n "${body_line}" ]] \
		|| fail "the shared_intent_lock kill-worker wrapper never calls _ifa_generic_cell_killworker_body"

	[[ "${precondition_line}" -lt "${body_line}" ]] \
		|| fail "the shared_intent_lock precondition runs at or after the kill-worker body (precondition line ${precondition_line}, body line ${body_line}); it must gate the cell, not follow it"
}
