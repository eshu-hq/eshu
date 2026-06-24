// Deliberate import-order violation fixture.
//
// `react` is an external dependency and would normally alphabetize before
// `node:fs`, but the rule's group ordering also requires a blank line
// between builtin and external groups. Placing the builtin `node:fs`
// import AFTER the external `react` import violates both clauses.
import { useState } from "react";
import * as fs from "node:fs";

export const readIt = (): number => {
  const [value] = useState<number>(0);
  return value + fs.statSync.length;
};
