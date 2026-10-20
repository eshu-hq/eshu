// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package macie maps Amazon Macie metadata into AWS cloud collector facts.
//
// The package owns scanner-level fact selection for the Macie account session
// status, member accounts (email-free), classification-job metadata (identity,
// type, status, and a bucket-criteria-summary count), allow-list identities,
// custom data identifier identities, findings filter identities, and aggregate
// finding counts by severity. It emits reported evidence only.
//
// Macie is the highest-redaction scanner in the AWS collector. Sensitive-data
// findings are the personally identifiable information Macie detected, custom
// data identifier regular expressions are descriptions of that data, allow-list
// contents and findings filter criteria reveal detection posture, and
// classification-job bucket-criteria expressions and explicit bucket lists
// reveal the targets of a scan. None of those enter the scanner contract: the
// scanner-owned domain types carry identity and counts only, so the sensitive
// payloads are unreachable by construction.
package macie
