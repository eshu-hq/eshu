// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// formatDate is reached only through the "@acme/format-kit/dates" subpath,
// which the parser does not key, so it gains no cross-repository caller.
export function formatDate(date) {
  return date.toISOString().slice(0, 10);
}
