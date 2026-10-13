// Package b closes the fixture with a type-only import back to a,
// which never runs at load time and must close no cycle.
import type { A } from "@cycles-typeonly/a";

export type BAlias = A;
export function B(): string {
  return "b";
}
