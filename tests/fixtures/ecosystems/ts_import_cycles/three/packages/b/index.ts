// Package b continues the three-package cycle: b -> c.
import { C } from "@cycles-three/c";

export function B(): string {
  return C();
}
