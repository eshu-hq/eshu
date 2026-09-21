#!/usr/bin/env bash
# shellcheck disable=SC1090,SC2034,SC2154
# Hermetic proof for the forced-expiry re-claim assertion used by cell 3
# (expire-lease-mid-handler). The parent mirror owns strict mode, fail(),
# repo_root, and the library path variables.
#
# These are behavioural cases, not source pins. The pins in
# test-ifa-fault-injection-cell-pins-cases.sh prove cell 3 CALLS the assertion;
# only these prove the assertion itself fails when the re-claim never happened.
# A pin alone would be satisfied by a helper that always returns 0.

run_ifa_fault_injection_reclaim_assert_cases() {
	test_ifa_reclaim_assert_fails_when_expiry_caused_no_reclaim
	test_ifa_reclaim_assert_passes_on_strict_increase
	test_ifa_reclaim_assert_rejects_equal_count
	test_ifa_reclaim_count_is_domain_agnostic
}

# The case this whole change exists for: the forced expiry ran, nothing was
# re-claimed, and the cell must NOT report success.
test_ifa_reclaim_assert_fails_when_expiry_caused_no_reclaim() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml

	ifa_det_pg() { printf '4'; }

	if ifa_fault_assert_reclaimed_above test-project 0 test-dsn test-compose.yml 4 1 >/dev/null; then
		fail "ifa_fault_assert_reclaimed_above returned success when the re-claimed count never rose above the pre-expiry baseline -- cell 3 would pass without exercising the race"
	fi
)

test_ifa_reclaim_assert_passes_on_strict_increase() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml observed

	ifa_det_pg() { printf '  7\n'; }

	observed="$(ifa_fault_assert_reclaimed_above test-project 0 test-dsn test-compose.yml 4 1)" \
		|| fail "ifa_fault_assert_reclaimed_above rejected a genuine increase (baseline 4, observed 7)"
	[[ "${observed}" == "7" ]] \
		|| fail "ifa_fault_assert_reclaimed_above printed ${observed}, want the whitespace-stripped count 7"
)

# Equality is not evidence. If the count did not move, no row gained a second
# claim while this cell's expiry was in force.
test_ifa_reclaim_assert_rejects_equal_count() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml

	ifa_det_pg() { printf '5'; }

	if ifa_fault_assert_reclaimed_above test-project 0 test-dsn test-compose.yml 5 1 >/dev/null; then
		fail "ifa_fault_assert_reclaimed_above accepted an unchanged count as proof of a re-claim"
	fi
)

# cell 3 expires the lease on EVERY claimed/running reducer row, not on one
# named domain, so the count must not be domain-scoped the way
# ifa_fault_count_retried is. A domain filter here would miss the re-claim
# whenever the reducer happened to be holding a different domain's row.
test_ifa_reclaim_count_is_domain_agnostic() (
	# shellcheck source=scripts/lib/ifa_fault_reclaim_assert.sh
	source "${reclaim_assert_lib}"
	local FAULT_COMPOSE_PROJECT=test-project use_compose=0 ESHU_POSTGRES_DSN=test-dsn
	local compose_file=test-compose.yml seen="" sql_capture
	# The helper calls ifa_det_pg inside $(...), so a variable the stub assigns
	# is lost with the subshell. Capture through a file instead.
	sql_capture="$(mktemp)"

	ifa_det_pg() {
		printf '%s' "$4" >"${sql_capture}"
		printf '1'
	}

	ifa_fault_count_reclaimed test-project 0 test-dsn test-compose.yml >/dev/null
	seen="$(cat "${sql_capture}")"
	rm -f "${sql_capture}"
	[[ "${seen}" == *"stage = 'reducer'"* ]] \
		|| fail "re-claim count query is not scoped to the reducer stage: ${seen}"
	[[ "${seen}" == *"attempt_count > 1"* ]] \
		|| fail "re-claim count query does not count rows claimed more than once: ${seen}"
	# Match the bare word, not "domain =". The spaced form let a no-space
	# `domain='x'` filter through, which is the same silent-zero regression
	# this case exists to catch. Safe to match bare: the query contains no
	# "domain" substring at all.
	[[ "${seen}" != *"domain"* ]] \
		|| fail "re-claim count query filters on a single domain, but cell 3 expires the lease across every domain: ${seen}"
)
