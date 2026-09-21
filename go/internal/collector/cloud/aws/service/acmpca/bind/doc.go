// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind binds the ACM Private CA (acm-pca) service scanner into
// the awsruntime registry. Importing this package for its init side effect adds
// the production scanner to the registry without modifying any shared file.
//
// The acm-pca scanner is metadata-only and reads no operator free-text, so the
// registration leaves RequiresRedactionKey unset; the runtime derives no
// redaction-key pre-flight requirement for this service kind.
package runtimebind
