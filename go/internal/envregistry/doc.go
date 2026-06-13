// Package envregistry is the code-owned source of truth for Eshu's ESHU_*
// environment variables. It declares each supported variable with its type,
// default, owning subsystem, and deprecation aliases, and it powers
// `eshu config validate` and the generated environment-variable reference doc
// (docs/public/reference/env-registry.md).
//
// Scope: the registry covers the core platform subsystems — postgres, graph,
// runtime, api, mcp, reducer, ingester, projector, coordinator, semantic, and
// component. Collector and registry-credential variables are a separate, larger
// surface tracked outside this package. The coverage test
// (TestRegistryCoversCoreEnvCallSites) is deliberately scoped to the core
// config files so the registry stays authoritative for exactly what it claims
// to cover, rather than silently drifting.
//
// Validation classifies findings into three kinds: invalid values for known
// variables (errors), deprecated variables or aliases (warnings), and unknown
// variables. Unknown variables are reported only when they closely resemble a
// known name (a likely typo) or strict mode is requested, so legitimate
// out-of-scope variables do not produce noise.
package envregistry
