# Generator test examples

Paths in backticks are repository-relative.

## Idempotency Is A First-Class Test

A generator that produces different bytes on a clean re-run is a bug,
not a feature. The test mirror MUST include an idempotency case as
a check:

```bash
# Case 1: generator is idempotent — re-running with the same inputs
# produces the same bytes. (Deterministic output is the load-bearing
# property of the gate.) Use the byte-comparison form below, which is
# portable across macOS and the GHA ubuntu-latest runner.
if cmp -s "${output_path}" "${output_path}.bak"; then
  record_pass "generator is idempotent on a clean re-run"
else
  record_fail "generator output is not byte-for-byte deterministic"
fi
```

The byte-comparison form matches the convention used by
`scripts/test-verify-telemetry-coverage.sh` and
`scripts/test-generate-operator-dashboard.sh`. Capture the expected
output once with `cp "${output_path}" "${output_path}.bak"` before
running the second pass; if the second pass produces the same bytes,
the generator is deterministic. Do not use `md5 -q` (macOS-only) or
`md5sum` (Linux-only) — `cmp -s` works on both and is the repo
convention.

This catches timestamp embedding, hostname leaks, unkeyed `map`
iteration in templating languages, and any other non-determinism that
would otherwise only show up when CI runs.

## Test Cases That Catch Real Bugs

The cases in `scripts/test-verify-telemetry-coverage.sh` and
`scripts/test-generate-operator-dashboard.sh` are the worked examples. Run them
for the current count — both print `tests passed: N/N` — rather than trusting a
number written here, which is how the counts quoted in this section went stale.
The patterns that caught real bugs:

- **Idempotency** (above).
- **Top-level shape**: parse the JSON / YAML with `jq` or `yq` and
  assert `title`, `uid`, `schemaVersion`, or schema-required keys
  are present.
- **Cross-link enforcement**: for every data name in the registry,
  assert the generated output references it. This is the link
  between "the source of truth changed" and "the artifact kept up".
  For the dashboard, every `eshu_dp_*` in the metrics lib must
  appear in some panel expression.
- **Negative cases**: at least one negative case that proves the
  script can fail. The "doc references unregistered metric" case in
  the X2 test mirror is the model.
