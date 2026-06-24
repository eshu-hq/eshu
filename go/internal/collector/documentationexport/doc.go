// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package documentationexport parses explicit offline issue, ticket, and chat
// export files into source-neutral documentation facts.
//
// Callers must supply an import manifest and exact file bytes; the package does
// not discover files, open archives, call live providers, or persist
// credentials. Manifest preflight is fail-closed, so any unsafe source,
// allowlist, ACL, path, attachment, private-channel, or sensitive-value warning
// returns no facts. After manifest approval, malformed or unsupported records
// become metadata-only document facts with no sections. Record bytes feed
// document content hashes, section truncation preserves valid UTF-8, unknown
// scope kinds are fingerprinted, and source-native link section IDs are not
// retained. Emitted facts describe document evidence only and do not create
// work-item, incident, deployment, ownership, or graph truth.
package documentationexport
