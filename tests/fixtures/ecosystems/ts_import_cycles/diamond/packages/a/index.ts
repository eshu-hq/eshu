// Package a opens the diamond negative fixture for #7348:
// a -> b, a -> c, and no path back.
import { B } from "@cycles-diamond/b";
import { C } from "@cycles-diamond/c";

export function A(): string {
  return B() + C();
}
