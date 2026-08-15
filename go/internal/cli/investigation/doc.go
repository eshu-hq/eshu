// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package investigation builds the artifact behind `eshu investigation export`:
// a portable investigation_evidence_packet.v2 for one of three families
// (supply_chain_impact, deployable_unit, drift).
//
// BuildPacket is the entry point. It takes a Request naming the family, the
// scope the operator asked about, and an optional bounds override, reads the
// matching API route through Deps, and returns a packet the caller renders with
// query.RenderInvestigationPacket.
//
// Two results are both successes. A packet whose Refusal is set is a valid,
// share-safe artifact stating why the question could not be answered: an
// unrecognized family, a scope the API cannot resolve, an unavailable backend,
// or a profile that does not serve the capability. A returned error means no
// honest artifact could be produced and the operator reads the message instead.
// Callers must not treat a refusal as a failure, and must not treat an error as
// an empty answer.
//
// Three rules the callers depend on:
//
// A transport error is classified by HTTP status alone, through
// apierr.StatusCode. This package inspects no error text, which is where it
// differs from `eshu trace`; see RefusalFromFetchError.
//
// Fetches return the transport error unwrapped, so a status survives
// errors.As and the operator reads the client's own message.
//
// Server- and transport-supplied text never enters the packet. An envelope
// error message and a transport error string reach stderr only. The scope the
// operator named does enter the packet, by design, so a --subject value is not
// a safe place for a credential.
//
// The package holds no cobra dependency, reads no environment, and maps nothing
// to an exit code. Flag reading, stream resolution, and exit-code mapping stay
// in go/cmd/eshu/investigation_cmd.go.
package investigation
