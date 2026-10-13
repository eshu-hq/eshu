// Second file in package a: same-package files must collapse
// into one node, so this second a -> b edge reports no new cycle.
import { B } from "@cycles-two/b";

export function AExtra(): string {
  return B();
}
