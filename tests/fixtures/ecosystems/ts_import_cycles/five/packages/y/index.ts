// Package y continues the five-package cycle: y -> z.
import { Z } from "@cycles-five/z";

export function Y(): string {
  return Z();
}
