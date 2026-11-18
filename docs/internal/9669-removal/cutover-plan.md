# 9669 cutover lane plan (traffic flip: satellite becomes the serving path)

Base: core `1650d11170` (D13 merge, post-removal tree — `go/cmd/api` is
gone). Satellite tip `c041d7e` (frozen G9 triple, full surface). IaC main
`28394c5`. QA cluster verified live on 2026-11-18 with the
authorized SSO role (see session record for identity).

## Verified current state (all observed, not inferred)

- QA `eshu` namespace serves the API from the monolith image
  `ghcr.io/eshu-hq/eshu:sha-c8356f1e6d408948c96699e60d1edf0778f1195e@sha256:92c2…`
  (pre-removal build; contains the deleted in-tree relay). Service `eshu:8080`.
- QA satellite `eshu-extracted-api` runs release image
  `ghcr.io/eshu-hq/eshu-api@sha256:b14d868e…` (v0.5.0-era flip pin, IaC
  `app-values.yaml:346-350`) with `--core-base-url http://eshu:8080` —
  the satellite is a dispatch shim in front of the monolith.
- QA `eshu-mcp-server` (`eshu-mcp:0.2.15`) also cores on `http://eshu:8080`.
- Only `eshu-extracted-api` and `eshu-mcp-server` dial the monolith
  in-cluster. No Gateway API route exposes the API externally (only
  `mcp-eshu.qa.ops.bgrp.io` + webhooks); serving proof runs inside the
  cluster / CI live-satellite envs.
- Satellite v0.5.0 (2026-11-09) predates waves #152-154 (row-7/full
  surface, merged 11-16/17). No post-surface satellite release exists.
- No core service exists anywhere: no `go/cmd/*` binary serves
  `CoreReadContractsVersion` over HTTP; the satellite repo is HTTP-layer
  only by ADR-0001. The 26 relay rows (generic proxy, `api/router.go:472`
  catch-all) have no post-D13 server.
- ArgoCD application for the QA environment (label selector `app.kubernetes.io/instance: eshu`) sources `iac-eks-eshu` (chart +
  values, manual sync). The monolith image can never be rebuilt from
  post-D13 source (Dockerfile builds the satellite at `/go-bin/eshu-api`).

## What the replacement must actually answer (corrected 2026-11-18)

An earlier version of this section claimed the relay set was empty
and nothing needed re-homing. That measured routing only: all 279
operations do match native registrations (scope tests pass). But the
native handlers forward to the old service for the answer
(`serveCodeFlowTerminatedAs` and siblings: authorize, then
`forwardToCore`, core bytes back unmodified). Routing is new; the
answers still come from the old place.

So "the 26" is the wrong number in both directions: as routes there
is nothing to re-home, but as serving work the replacement must
answer everything the new service passes along -- essentially the
whole read surface, not 26 rows. The contract for that surface is
the frozen read-method set (G2 pins + parity stub wire strings
under `CoreReadContractsVersion`), with the deleted in-tree
handlers as behavioral oracle (reference only).

## Goal

Replace the monolith as the serving core so traffic (satellite +
MCP entrypoints, then any direct monolith callers) is served without
the deleted relay. Non-goals: collector/reducer binaries keep running
from pinned monolith images; no console changes unless its QA base
URL points at the monolith (verify during phase 0).

## The core decision (needs ruling before building)

- Option A — new core service (core repo `go/cmd/...`, serves the
  G2-pinned read contracts over HTTP from `go/internal/query` +
  stores). Full program fit; new binary, wiring (handlers were deleted
  with `go/cmd/api`), gates, image, chart, parity pins.
- Option B — fully-native satellite (re-home the 26 relay rows to
  viaNative handlers, one pin per leaf per standing discipline; core
  flag stays but nothing relays). Smaller deploy footprint; needs
  satellite-repo lanes + a `--core-base-url`-optional decision there.
- Rejected: freezing the monolith image as the permanent core (source
  deleted, image unrebuildable, migrations keep moving).

## Ordered phases (each gated, no phase starts until the prior is proven)

0. Inventory: enumerate every direct monolith caller (console QA base
   URL, external QA harnesses, CI live-satellite envs); record the
   flip surface.
1. Satellite release: cut post-full-surface release (≥`c041d7e`) with
   binary + image assets (unblocks Dockerfile row-11 COPY + IaC digest).
2. Core path per ruling (A: build+gate+image+chart; B: native lanes).
3. IaC: core Deployment/Service + repoint satellite + MCP
   `coreBaseURL` + verify (no traffic moved yet; old path intact).
4. Parity: authed parity full-match on QA against the new path
   (G2 pins + golden-corpus serving legs restored to point at it).
5. Flip: move entry to the satellite path; bake; decommission the
   monolith api Deployment (image pin stays for collectors/reducer).
6. Rollback: every phase keeps the monolith path runnable until phase
   5 bakes; rollback = repoint + sync (no rebuild needed).

## Phase-0 findings (recorded 2026-11-18, live cluster evidence)

- In-cluster direct monolith callers: exactly two — satellite
  `eshu-extracted-api` and `eshu-mcp-server`, both
  `--core-base-url http://eshu:8080`. Both repoint in phase 3.
- `eshu-api-commit:8089` selects the same monolith pods (commit-binding
  endpoint, part of monolith serving — retires with it).
- No eshu console deployed in-cluster (only observability frontends);
  no Gateway API route exposes the API externally (only MCP + webhooks
  have hostnames). No external callers to flip; the console takes its
  base URL at runtime (bootFromSession, operator-pointed), so the flip
  communicates the new endpoint rather than rewiring infra.
- CI live-satellite envs use mock cores (no cluster dependency).

## Ruling request (owner/arbiter)

1. Core shape: A (new core service) or B (fully-native satellite)?
2. Confirm phase order + parity bar (authed full-match on QA, G2 pins green).
3. Standing grants suffice (PRs, merges on review+green, QA EKS access)?

No traffic moves and no cluster writes happen until the ruling lands.
