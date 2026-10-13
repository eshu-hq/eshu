// Package w continues the five-package cycle: w -> x.
import { X } from "@cycles-five/x";

export function W(): string {
  return X();
}
