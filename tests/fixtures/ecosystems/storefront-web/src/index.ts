// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

import { formatPrice, Money } from "@acme/format-kit";
import type { Ledger } from "@acme/format-kit";
import { formatDate } from "@acme/format-kit/dates";

// Same name as format-kit's private helper; this call stays in this repository.
function roundCents(value: number): number {
  return Math.round(value);
}

// Money and Ledger appear only as type annotations, never as calls.
export function balance(amount: Money, ledger: Ledger): number {
  return roundCents(amount.cents) + ledger.entries.length;
}

// main is the package entry point. Its formatPrice call is the cross-repository
// edge; formatDate comes through a subpath import and stays unkeyed.
export function main(cents: number): string {
  return `${formatPrice(roundCents(cents))} ${formatDate(new Date(0))}`;
}
