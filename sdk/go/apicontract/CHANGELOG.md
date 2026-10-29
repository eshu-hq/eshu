# Changelog

All notable changes to `github.com/eshu-hq/eshu/sdk/go/apicontract` are
documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
The module version tracks the highest bump any ported shape needs: a renamed
field, retag, narrowed type, or changed meaning is a major bump; an additive
optional field or entry is a minor bump; docs-only is a patch. See
[SDK Compatibility](../../../docs/public/extend/sdk-compatibility.md) for how
this module version lines up with core Eshu releases.

Tags for this module use the Go subdirectory-module format:
`sdk/go/apicontract/vX.Y.Z`.

## [Unreleased]

## [0.1.0] - 2026-10-29

### Added

- Initial module (Refs #9509, part of #6707): publishes the API wire shapes
  a thin CLI needs — response/truth/error envelopes, query profiles, graph
  backends, freshness causes, the MCP tool-definition shape, and the
  client-scoped environment registry with its validator — with JSON goldens
  pinned to the live core types and drift tests on both sides of the module
  boundary. Release-prepared in the `drive/8874-tier2-population` lane
  (Tier-2 owner-approved 2026-10-29; Refs #8874, Refs #9509, part of #6707);
  the `sdk/go/apicontract/v0.1.0` tag is NOT cut here — a maintainer cuts it
  per [`RELEASING.md`](../../../RELEASING.md) after merge.
