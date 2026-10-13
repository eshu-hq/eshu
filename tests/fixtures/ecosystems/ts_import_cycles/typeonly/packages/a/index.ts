// Package a opens the type-only negative fixture for #7348:
// a -> b runs at load time, but b -> a is import type only.
import { B } from "@cycles-typeonly/b";

export function A(): string {
  return B();
}
