// Package d is the diamond sink: an external import closes nothing.
import { useState } from "react";

export function D(): string {
  useState();
  return "d";
}
