// Package z closes the five-package cycle: z -> v.
import { V } from "@cycles-five/v";

export function Z(): string {
  return V();
}
