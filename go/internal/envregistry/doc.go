// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package envregistry is the code-owned source of truth for Eshu's ESHU_*
// environment variables. It declares each supported variable with its type,
// default, owning subsystem, and deprecation aliases, and it powers
// `eshu config validate` and the generated environment-variable reference doc
// (docs/public/reference/env-registry.md).
//
// Scope: the registry covers the core platform subsystems (postgres, graph,
// runtime, api, mcp, reducer, projector, coordinator, semantic, component) and
// the hosted-collector production configuration. Container-registry credential
// variables (ESHU_*_OCI_*, ESHU_*_PACKAGE_*) are integration-test gating read
// only from _test.go and are out of scope.
//
// One exception, and it is a rule rather than a special case: a contributor
// test knob that is cited on a docs/public page IS carried here, because the
// docs-cli-env-refs ratchet requires code ownership for every public citation
// and its debt baseline is frozen against a ceiling that can never grow. Such
// knobs get their own Subsystem (see backend-conformance) so they render in a
// section of their own rather than beside operator runtime settings. The
// ESHU_*_OCI_* variables above look like a counter-example but are not -- they
// are grandfathered debt inside that frozen ceiling, not a pattern to copy.
//
// The coverage test
// (TestRegistryCoversCoreEnvCallSites) scans the core, collector, and split
// command config files, so the registry stays authoritative for exactly what it
// claims to cover rather than silently drifting.
//
// Validation classifies findings into three kinds: invalid values for known
// variables (errors), deprecated variables or aliases (warnings), and unknown
// variables. Unknown variables are reported only when they closely resemble a
// known name (a likely typo) or strict mode is requested, so legitimate
// out-of-scope variables do not produce noise.
package envregistry
