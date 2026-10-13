// Package b closes the two-package cycle fixture for #7348:
// b -> a.
import { A } from "@cycles-two/a";

export function B(): string {
  return A();
}
