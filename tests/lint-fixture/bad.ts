// Deliberate ESLint violation fixture.
//
// This file is intentionally non-conformant. It exists to prove that the
// flat ESLint configuration at the repo root is wired up to the rule set
// the issue calls for (no-console, no-unused-vars). It must NOT be linted
// by `npm run lint` (it is excluded in `eslint.config.js`'s top-level
// `ignores`) but it MUST be linted by `scripts/test-eslint-config.sh`,
// which asserts a non-zero exit and specific rule ids in the report.
//
// Keep this file exactly as bad as it is — the test depends on every
// violation listed below firing.
import { useState } from "react";

const trulyUnused = 42;

const deliberateConsoleLog = (): number => {
  console.log("deliberate lint violation: console.log must be flagged");
  const [value] = useState<number>(0);
  return value;
};

export { deliberateConsoleLog };
