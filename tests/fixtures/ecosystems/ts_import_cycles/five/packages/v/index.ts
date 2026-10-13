// Package v opens the five-package cycle fixture for #7348.
import { W } from "@cycles-five/w";

export function V(): string {
  return W();
}
