// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package semantic owns semantic-evidence source fact kinds: LLM-assisted
// documentation observations and code hints, their payload types, stable-id
// derivation, payload validation, and SDK factschema encoding.
//
// The kinds declared here are source evidence only. Reducers and the
// semantic read model own graph truth; this package decides no admission
// and projects nothing. The family declaration uses contract.Declare, the
// identity function the static registry generator reads; see the fact-kind
// registry for the generated kind rows.
//
// The package depends on facts/contract, facts/docs, facts/encode, and the
// SDK factschema module. It must not import the parent facts package: the
// schema registry and family callers reach in through this package, never
// the reverse. Payload encoding goes through encode.StableID, the shared
// implementation the root facts.StableID forwarder also uses, so ids stay
// byte-identical across the move.
package semantic
