// Package a opens the two-package cycle fixture for #7348:
// a -> b -> a.
import { B } from "@cycles-two/b";

// A calls into package b.
export function A(): string {
  return B();
}
