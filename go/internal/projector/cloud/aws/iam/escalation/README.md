# IAM escalation projector intents

## Purpose

This package recognizes AWS `aws_iam_permission` identity statements for one
scope generation and builds the reducer intent that asks the reducer to
project them into conservative `CAN_ESCALATE_TO` privilege-escalation edges
between committed IAM `CloudResource` nodes (#6228).

## Ownership boundary

The package owns only the qualifying-fact trigger selection, the typed decode
that selection needs, and the reducer-intent value. The root
`internal/projector` package validates scope-generation boundaries, constructs
and owns the immutable fact lookup, preserves family order, and owns
projection lifecycle, queue writes, retries, and telemetry. The reducer's
`DomainIAMEscalationMaterialization` handler
(`IAMEscalationMaterializationHandler`) owns the closed primitive catalog,
the bounded ARN join, the skip taxonomy, the canonical-nodes readiness check,
the backend-neutral edge write, and readiness publication.

## Exported surface

- `BuildIAMEscalationMaterializationReducerIntent` builds the
  `iam_escalation_materialization` intent, anchored to the earliest fact in
  the generation, across candidate identity statements in original input
  order, that is an `aws_iam_permission` fact whose payload decodes and
  carries `policy_source` `"inline"` or `"attached_managed"`. Either effect
  qualifies: an Allow can arm a catalog primitive and a Deny contributes to
  the grant's deny set.

See `doc.go` for the full godoc contract.
