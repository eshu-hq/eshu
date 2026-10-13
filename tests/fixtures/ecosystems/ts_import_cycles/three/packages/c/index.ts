// Package c closes the three-package cycle: c -> a.
import { A } from "@cycles-three/a";

export function C(): string {
  return A();
}
