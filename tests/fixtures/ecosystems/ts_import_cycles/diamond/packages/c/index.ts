// Package c is the right diamond arm: c -> d.
import { D } from "@cycles-diamond/d";

export function C(): string {
  return D();
}
