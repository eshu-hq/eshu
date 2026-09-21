// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package runtimebind binds the Service Catalog service scanner into the
// awsruntime registry. Importing this package for its init side effect adds the
// production scanner to the registry without modifying any shared file.
package runtimebind
