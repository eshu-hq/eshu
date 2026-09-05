---
name: generator-script-discipline
description: Build or change deterministic generators, checked-in artifacts, and their drift checks in Eshu.
---

# Generator script discipline

A generator must produce identical bytes from identical inputs, keep its
committed output current, and fail visibly when the output contract is broken.
This applies to generators and their data registries, templates, tests, and CI
checks, including generators written outside shell.

Keep rendering code separate from substantial data or templates when that makes
changes clearer. Every file remains under the repo's 500-line cap; a tiny
generator does not need empty library fragments to satisfy a prescribed layout.
Provide a test mirror that runs without Postgres, NornicDB, or a Go build.

## Proof and integration

- Test byte-for-byte idempotency (`cmp -s` is portable), required output shape,
  source-registry cross-links, and a meaningful negative case.
- Regenerate changed artifacts before final verification and include the output
  in the change. Re-running must produce no further drift.
- For a release-blocking drift check, add the test and gate commands to
  `.github/workflows/static-contract-gates.yml` by default. Use a standalone
  workflow only for trigger, permission, or artifact-upload needs that the
  shared matrix cannot support. Read [CI integration](references/ci.md) for
  that exceptional workflow shape.
- Run the affected generator's test mirror and drift gate. Add docs validation
  when documentation changes; preserve the repository's required gate floor.

Read [test examples](references/testing.md) when implementing or extending a
mirror. For shell generators that render substantial template bodies, read
[shell portability](references/shell-portability.md) before choosing the output
mechanism. It records the Homebrew/Linuxbrew heredoc hang and the template-file
workaround; retain bounded watchdog tests for that failure mode.

Use `LC_ALL=C` and stable ordering where output depends on locale. Validate JSON
with the repo's `jq` convention. Reference generated JSON in MkDocs prose when
it is not a documentation page; do not add a broken page link.
