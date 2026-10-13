# Shared-shallow cases (eshu-hq/eshu#8048), sourced by
# scripts/test-verify-telemetry-coverage.sh -- not executed directly. Reuses
# that suite's init_repo, verifier, verifier_out/verifier_err, tmp_root,
# record_pass, and record_fail helpers.
#
# Resolving the CI base over the network must not change the depth of a
# complete checkout. Every worktree shares one .git/shallow, so a --depth
# fetch in this gate shallows all lanes and fails unrelated lanes'
# methodology identity check. The fixture is a full file:// clone whose base
# branch exists only on the remote side: the gate must fetch it (the
# origin/pr-base assertion proves the fetch still happens) and the checkout
# must stay complete.

shallow_seed="$(init_repo shallow-seed)"
git -C "${shallow_seed}" branch pr-base
git clone --quiet "file://${shallow_seed}" "${tmp_root}/shallow-clone"
shallow_clone="${tmp_root}/shallow-clone"

if [ "$(git -C "${shallow_clone}" rev-parse --is-shallow-repository)" != "false" ]; then
  record_fail "shallow fixture clone started shallow"
elif env -u ESHU_TELEMETRY_COVERAGE_BASE -u GITHUB_BASE_REF \
  ESHU_TELEMETRY_COVERAGE_REPO_ROOT="${shallow_clone}" \
  GITHUB_BASE_REF="pr-base" \
  "${verifier}" >"${verifier_out}" 2>"${verifier_err}"; then
  if [ "$(git -C "${shallow_clone}" rev-parse --is-shallow-repository)" != "false" ]; then
    record_fail "resolving the CI base rewrote shared shallow state"
  elif git -C "${shallow_clone}" rev-parse --verify --quiet "origin/pr-base" >/dev/null; then
    record_pass "resolving the CI base keeps a complete checkout complete"
  else
    record_fail "resolving the CI base left origin/pr-base unresolved"
  fi
else
  record_fail "resolving the CI base failed on a complete checkout"
fi
unset shallow_seed shallow_clone
