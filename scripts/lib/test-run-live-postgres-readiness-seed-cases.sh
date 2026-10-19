#!/usr/bin/env bash
# Seeded-failure cases for the PostgreSQL readiness runner self-test: the
# results verifier must fail closed on an empty or malformed PACKAGES.
# Sourced by scripts/test-run-live-postgres-readiness-tests.sh, which defines
# fail, repo_root, checker, seed_dir, last_proof and last_proof_module.
# shellcheck disable=SC2154  # the sourcing test defines these variables.

# list-packages fails closed on an empty or malformed PACKAGES.
mkdir -p "${seed_dir}/empty" "${seed_dir}/malformed"
for location in empty malformed; do
  cp "${checker}" "${seed_dir}/${location}/live_postgres_readiness_results.py"
  cp "${repo_root}/scripts/lib/live_postgres_readiness_events.py" "${seed_dir}/${location}/"
  cp "${repo_root}/scripts/lib/live_tests_registry.py" "${seed_dir}/${location}/"
  cp "${repo_root}/scripts/lib/live_postgres_readiness_records.py" "${seed_dir}/${location}/"
  cp "${repo_root}"/scripts/lib/live_postgres_readiness_inventory*.py "${seed_dir}/${location}/"
done
# The assembler deep-merges per-package tables (sharded packages share one
# key), so emptying means dropping the merge loop, not a brace block.
sed '/^PACKAGES = {}$/,$c\PACKAGES = {}' "${repo_root}/scripts/lib/live_postgres_readiness_inventory_generated.py" \
  >"${seed_dir}/empty/live_postgres_readiness_inventory_generated.py"
cmp -s "${repo_root}/scripts/lib/live_postgres_readiness_inventory_generated.py" "${seed_dir}/empty/live_postgres_readiness_inventory_generated.py" && fail "empty seed changed nothing"
out="$(python3 "${seed_dir}/empty/live_postgres_readiness_results.py" list-packages 2>&1)" && fail "empty PACKAGES listed"
[[ "${out}" == *"PACKAGES is empty"* ]] || fail "empty PACKAGES not named: ${out}"
sed "s/\"${last_proof}\"/\"not a test\"/" "${last_proof_module}" >"${seed_dir}/malformed/$(basename "${last_proof_module}")"
cmp -s "${last_proof_module}" "${seed_dir}/malformed/$(basename "${last_proof_module}")" && fail "malformed seed changed nothing"
out="$(python3 "${seed_dir}/malformed/live_postgres_readiness_results.py" list-packages 2>&1)" && fail "malformed test name listed"
[[ "${out}" == *"malformed test"* ]] || fail "malformed test not named: ${out}"
