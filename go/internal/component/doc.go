// Package component provides local component package metadata contracts.
//
// The package validates component manifests, applies local trust policy, and
// stores installed package and activation state for optional Eshu collectors
// and services. Installation is intentionally inert: runtime launch and work
// claiming are modeled as separate activation decisions. Runtime metadata
// declares the collector SDK protocol and adapter a host must understand before
// an activation can become claim-capable. Registry readback reports
// deterministic lifecycle states, and classified errors give CLI and automation
// callers stable failure codes without exposing private paths. Manifest
// fact-family metadata declares schema versions and non-unknown
// source-confidence values before a component can be installed.
package component
