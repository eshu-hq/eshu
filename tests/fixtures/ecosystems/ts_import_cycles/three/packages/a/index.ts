// Package a opens the three-package cycle fixture for #7348:
// a -> b -> c -> a.
import { B } from "@cycles-three/b";

export function A(): string {
  return B();
}
