// Package component provides local component package metadata contracts.
//
// The package validates component manifests, applies local trust policy, and
// stores installed package and activation state for optional Eshu collectors
// and services. Installation is intentionally inert: runtime launch and work
// claiming are modeled as separate activation decisions.
package component
