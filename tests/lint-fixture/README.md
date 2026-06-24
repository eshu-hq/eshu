# Deliberate ESLint violation fixtures

This directory holds TypeScript files that intentionally violate the ESLint
rules configured in `eslint.config.js` at the repo root.

The files here are excluded from the regular `npm run lint` gate via the
top-level `ignores` block of the flat config, because they exist solely to be
fed to ESLint from the test script that proves the gate catches the violations
it is supposed to catch:

- `scripts/test-eslint-config.sh` runs ESLint against `bad.ts` and asserts a
  non-zero exit plus the specific rule ids (`no-console`, `no-unused-vars`)
  in the report. Then it runs ESLint against a clean sibling fixture and
  asserts zero exit. Together, the two cases prove the gate fails on
  violations and passes on clean code.

Do not add production code here, do not lint these files with `npm run lint`,
and do not remove the file unless the test script is updated at the same time.
