// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package signer maps AWS Signer (code-signing) signing-profile and
// signing-platform metadata into AWS cloud collector facts.
//
// The scanner emits reported-confidence resources for Signer signing profiles
// and signing platforms plus relationships for profile-to-ACM-certificate (the
// signing material certificate reference) and profile-to-signing-platform
// (the platform the profile is bound to) evidence. Signing jobs, signing
// material private keys, signed-object payloads, signing-parameter values,
// revocation records, and any mutation or signing API stay outside this package
// contract: the scanner is metadata-only.
package signer
