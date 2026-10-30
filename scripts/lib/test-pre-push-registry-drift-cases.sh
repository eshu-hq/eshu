#!/usr/bin/env bash
# Cases L1-L3 for scripts/test-pre-push.sh (#9615), sourced after its
# build_fixture/run_fixture helpers. The floor runs the registry drift check
# (scripts/verify-ci-gates-registry.sh --drift) on every push, not only when a
# ci-gate-registry trigger matches: #9604 added a scripts/lib module that two
# gates' commands import, no trigger matched it, so neither the selected-gates
# step nor the floor ran the import-drift check and CI was the first failure.
#
# Every case changes only README.md, which selects no ci-gate-registry trigger,
# so the step runs through the unconditional path.

# ── Case L1: a failing registry check fails the floor with no trigger match ──
fixture="$(build_fixture case-l1)"
status=0
: > "${fixture}.args"
DRIVER_ARGS_LOG="${fixture}.args" DRIVER_GATE_STATUS=0 DRIVER_REGISTRY_STATUS=1 \
	PATH="${fixture}/bin:${PATH}" bash "${fixture}/scripts/dev/pre-push.sh" > "${fixture}.log" 2>&1 || status=$?
[[ "${status}" != "0" ]] || { cat "${fixture}.log" >&2; fail "case L1: a failing registry drift check must fail the floor, got exit 0"; }
rg -q -- '^registry-drift ran --drift$' "${fixture}.args" ||
	{ cat "${fixture}.args" >&2; fail "case L1: the floor never ran verify-ci-gates-registry.sh --drift"; }
rg -q -- '^FAIL  ci-gate registry drift \(skipped if run above\)' "${fixture}.log" ||
	{ cat "${fixture}.log" >&2; fail "case L1: the summary does not name the failed registry drift step"; }

# ── Case L2: a clean registry check passes and still runs ──
fixture="$(build_fixture case-l2)"
status="$(run_fixture "${fixture}" 0)"
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case L2: expected exit 0, got ${status}"; }
rg -q -- '^registry-drift ran --drift$' "${fixture}.args" ||
	{ cat "${fixture}.args" >&2; fail "case L2: the floor never ran the registry drift check"; }
rg -q -- '^PASS  ci-gate registry drift \(skipped if run above\) \([0-9]+s\)$' "${fixture}.log" ||
	{ cat "${fixture}.log" >&2; fail "case L2: the summary does not carry the step's timing line"; }

# ── Case L3: ci-gate-registry already ran in the selected-gates step → the ──
# floor does not run it twice. The stand-in would fail, so a second run would
# turn the floor red; the gate step's own result is the one that counts.
fixture="$(build_fixture case-l3)"
status=0
: > "${fixture}.args"
DRIVER_ARGS_LOG="${fixture}.args" DRIVER_GATE_STATUS=0 DRIVER_GATE_RAN_REGISTRY=1 DRIVER_REGISTRY_STATUS=1 \
	PATH="${fixture}/bin:${PATH}" bash "${fixture}/scripts/dev/pre-push.sh" > "${fixture}.log" 2>&1 || status=$?
[[ "${status}" == "0" ]] || { cat "${fixture}.log" >&2; fail "case L3: expected exit 0 when the gate step already ran ci-gate-registry, got ${status}"; }
if rg -q -- '^registry-drift ran' "${fixture}.args"; then
	fail "case L3: the floor ran the registry drift check twice"
fi
rg -q -- 'ci-gate-registry already ran in the selected-gates step' "${fixture}.log" ||
	{ cat "${fixture}.log" >&2; fail "case L3: the skip was not reported"; }
