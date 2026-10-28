#!/usr/bin/env bash
# Focused tests for scripts/collector-cutover-rollback.sh (#8895).
#
# The shared harness and the case files live in scripts/lib/test-collector-
# cutover-rollback-*.sh so each file stays under the 500-line cap:
#   harness   PATH stubs, fake world builders, assertions
#   core      plan, step order, run guards, timeouts, idempotence, failing steps
#   gates     one refusal or ordering rule per case, a fact broken at a time
#   truth     verify, baseline format, capture, drift report, SQL pins
#   usage     input validation, environment knobs, help text
#   direct    the direct in-tree producer mode (#9027)
# scripts/lib/test-collector-cutover-rollback-mutants.sh proves these cases kill
# every listed mutant of the step and gate logic.
#   activations the activations/GitOps mode (#9293)

set -euo pipefail

lib_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/lib" && pwd)"

# shellcheck source=scripts/lib/test-collector-cutover-rollback-harness.sh
source "${lib_dir}/test-collector-cutover-rollback-harness.sh"
base_args
# shellcheck source=scripts/lib/test-collector-cutover-rollback-cases-core.sh
source "${lib_dir}/test-collector-cutover-rollback-cases-core.sh"
# shellcheck source=scripts/lib/test-collector-cutover-rollback-cases-gates.sh
source "${lib_dir}/test-collector-cutover-rollback-cases-gates.sh"
# shellcheck source=scripts/lib/test-collector-cutover-rollback-cases-truth.sh
source "${lib_dir}/test-collector-cutover-rollback-cases-truth.sh"
# shellcheck source=scripts/lib/test-collector-cutover-rollback-cases-usage.sh
source "${lib_dir}/test-collector-cutover-rollback-cases-usage.sh"
# shellcheck source=scripts/lib/test-collector-cutover-rollback-cases-direct.sh
source "${lib_dir}/test-collector-cutover-rollback-cases-direct.sh"
# shellcheck source=scripts/lib/test-collector-cutover-rollback-cases-activations.sh
source "${lib_dir}/test-collector-cutover-rollback-cases-activations.sh"

if [[ "${failures}" -gt 0 ]]; then
	printf '\n%d check(s) failed\n' "${failures}"
	exit 1
fi
printf '\nAll collector cutover rollback checks passed\n'
