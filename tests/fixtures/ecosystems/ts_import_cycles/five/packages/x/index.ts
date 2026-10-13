// Package x continues the five-package cycle: x -> y.
import { Y } from "@cycles-five/y";

export function X(): string {
  return Y();
}
