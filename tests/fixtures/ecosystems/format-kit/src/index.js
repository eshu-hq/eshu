// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// formatPrice is the export the storefront-web fixture calls by its bare
// package name, so its only caller lives in another repository.
export function formatPrice(cents) {
  return `$${(cents / 100).toFixed(2)}`;
}

// roundCents is not exported. storefront-web defines its own roundCents, and
// that call must stay inside storefront-web.
function roundCents(value) {
  return Math.round(value);
}

// formatRounded must not call formatPrice: a caller inside this repository
// would make formatPrice live locally and hide the cross-repository proof.
export function formatRounded(value) {
  return roundCents(value) / 100;
}

// Money and Ledger are used by storefront-web only as type annotations, which
// are not calls.
export class Money {
  constructor(cents) {
    this.cents = cents;
  }
}

export class Ledger {
  constructor(entries) {
    this.entries = entries;
  }
}
