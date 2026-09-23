// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package recordpolicy owns the AWS collector's record-mode pseudonym table:
// the mapping from every aws/v1 payload key to the recordpseudo.Class its
// reducer consumers tolerate. The engine (go/internal/replay/recordpseudo) is
// collector-neutral; this package is the collector's side of the contract.
//
// Policy() is the only export. It must list every key the aws/v1 JSON
// schemas declare (the package test walks sdk/go/factschema/schema/aws_*.json
// and fails on a missing key), so adding a payload field to the contracts
// module means classifying it here in the same change.
package recordpolicy
