// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package dockerfile extracts Dockerfile runtime evidence that can stay
// independent from the parent parser dispatch package.
//
// RuntimeMetadata returns typed stage, build-platform, argument, environment,
// port, label, and bounded RUN/ADD install declarations from Dockerfile source
// text, including quoted metadata values, legacy ENV key/value form, and
// top-of-file Dockerfile escape-directive handling. Metadata.Map preserves
// the parent parser payload shape used by query and relationship callers.
// ARG defaults declared before the first FROM are expanded into FROM
// base-image references; references whose
// value is only known at build time stay as written and are marked unresolved.
package dockerfile
