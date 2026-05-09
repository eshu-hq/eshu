# External Documentation Updater Actuator Contract Design

## Context

Eshu now has source-neutral documentation facts and a read-only Confluence
collector. The next boundary is the external updater service: a separate
process that reads Eshu documentation findings, asks an LLM to draft bounded
edits, renders diffs, and publishes only when policy allows it.

This work does not build that service inside Eshu. It defines the contract the
service must use so Eshu stays read-only.

## Decision

Add a reference contract document for external documentation updater actuators.
The contract will describe the API surface Eshu must expose, the evidence
packet shape the updater may snapshot, the expected error states, and the
permission split between Eshu and the destination publisher.

The contract will make three boundaries explicit:

- Eshu produces findings and evidence packets only.
- The updater owns LLM providers, prompts, writer modes, diffs, approvals, and
  destination publishers.
- Eshu core never stores LLM provider keys and never mutates documentation
  systems.

## Alternatives Considered

### Implement The Evidence Packet API First

This would move faster toward code, but it risks hard-coding the wrong
contract. The updater needs stable packet semantics before the API lands.

### Start The External Updater Repository

That repo will need this contract. Starting it first would push architecture
decisions into the wrong place and make Eshu's read-only boundary easier to
blur.

### Write The Contract First

This is the recommended path. It gives issue #71 a stable API target and gives
the future updater repo a clear trust boundary.

## Scope

This design adds documentation only:

- `docs/docs/reference/documentation-updater-actuator-contract.md`
- a navigation entry under Reference

It does not add HTTP routes, database tables, LLM calls, or destination writer
code.

## Validation

- The documentation contract must satisfy issue #69 acceptance criteria.
- The MkDocs strict build must pass.
