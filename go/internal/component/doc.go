// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package component provides local component package metadata contracts.
//
// The package validates component manifests, applies local trust policy, and
// stores installed package and activation state for optional Eshu collectors
// and services. Installation is intentionally inert: runtime launch and work
// claiming are modeled as separate activation decisions. Runtime metadata
// declares the collector SDK protocol and adapter a host must understand before
// an activation can become claim-capable. Strict trust mode can call a narrow
// provenance verifier to validate digest-pinned OCI artifact signatures and
// attestations without executing component code. Registry readback reports
// deterministic lifecycle states, and classified errors give CLI and automation
// callers stable failure codes without exposing private paths. Manifest
// fact-family metadata declares schema versions, optional payload-schema shape
// references, and non-unknown source-confidence values before a component can
// be installed; local registry checks reject core-owned fact kinds and report
// installed component fact-kind ownership collisions before install, dry-run
// enable, or activation. Core-issued producer grants relax that check for a
// named producer, version, kind, schema, and scope. Each grant allow or deny
// is reported to an optional GrantObserver at the install, readback, and
// activation stages (the extension host adds emission), classified into a
// closed set of reasons by ClassifyEmission; observation never changes the
// admission outcome.
package component
