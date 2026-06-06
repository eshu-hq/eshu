// Package reducer owns Eshu's cross-domain materialization, shared projection,
// queued repair, and reducer-owned fact publication.
//
// Reducer handlers admit candidates from committed facts, build canonical graph
// rows or reducer fact rows, publish graph-readiness phases, and preserve
// idempotency across retries and replays. They do not call graph drivers
// directly; canonical graph writes go through storage/cypher, and durable fact
// writes go through narrow writer interfaces wired by cmd/reducer.
//
// Changes in this package must preserve the evidence path from raw facts to
// admitted candidate, projected row, graph or fact write, and API/MCP query
// truth. Queue ordering, generation supersession, phase publication, repair
// flows, shared projection readiness, and truth-emitting domain registration
// are package-level contracts. SupplyChainImpactHandler also evaluates
// vulnerability.suppression facts and writes the resulting VEX or operator
// policy decision onto every impact finding; provider dismissals stay
// evidence and never auto-hide findings. The handler also computes an
// advisory-only safe-upgrade remediation per finding for ecosystems whose
// version and manifest semantics are represented in reducer matchers: it never
// auto-opens pull requests, and unsupported remediation remains explicit. The
// remediation block names the current version, vulnerable range, fixed-version
// source, match reason, first patched version, manifest-allows-fix decision,
// direct/transitive designation, parent package required for transitive
// upgrades, and an exact/partial/unknown confidence label so API and MCP
// callers can explain the upgrade path. Vendor-proven RPM, Debian/dpkg, and
// Alpine/APK remediation stays limited to parseable installed versions and
// single source-attributed fixed branches; missing provenance or ambiguous
// branches remain explicit missing evidence. Supply-chain impact version
// matching is ecosystem-aware for npm, Cargo, Pub, Swift, NuGet, Maven, and PyPI PEP 440
// exact-version evidence; unsupported or malformed ranges fail closed with
// explicit missing evidence. Security-alert reconciliation facts are keyed by provider alert
// identity, package identity, advisory ids, and provider evidence scope so
// provider-only placeholders are replaced by later matched or stale rows while
// preserving reason and evidence references for audit. They also carry the
// Eshu-owned observed package version and dependency evidence gaps used for
// reconciliation without copying provider alert fields into observed-version
// truth.
// S3 internet exposure materialization writes reducer-owned exposed /
// not_exposed / unknown posture properties onto existing S3 CloudResource nodes
// only, preserving unknown evidence as unknown rather than safe.
// EC2 internet exposure materialization does the same for existing EC2
// CloudResource nodes using EC2 posture, ENI topology, and security-group rule
// evidence without storing raw public IP addresses or treating missing topology
// as safe.
// IncidentRoutingMaterializationHandler writes exact PagerDuty
// IncidentRoutingEvidence graph rows only for safe declared/applied/live
// convergence or live-only no-IaC routing evidence; unsafe routing outcomes
// remain provenance-only.
package reducer
