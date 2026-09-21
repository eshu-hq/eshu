# Per-family projection stages

## Purpose

Reduce one scope generation's facts to a single family's rows and reducer
intents. There is one stage per family: files, parsed entities, relationships,
and workloads.

## Ownership boundary

Each stage is a pure function from an envelope slice to a result value. A
stage owns its own family's selection, deduplication and intent shape, and
nothing else: no writes, no queue, no retries, no telemetry. The projector
runtime (`../runtime`) owns invocation order and everything downstream of the
returned value; fact-kind selection helpers live in `../decode`.

## Exported surface

- `ProjectFileStage` / `FileStageResult`
- `ProjectEntityStage` / `EntityStageResult`
- `ProjectRelationshipStage` / `RelationshipStageResult`
- `ProjectWorkloadStage` / `WorkloadStageResult`

## Behavior worth knowing before changing anything here

A stage **skips** a fact it does not recognize rather than failing the
projection — recognition is the stage's own business, and a fact belonging to
another family is not an error. That is the opposite of the decode contract in
`../decode`, where an unparseable payload must be quarantined or returned
fatally. Do not carry one rule into the other.

Deduplication is per-stage and within-family. A stage that stops deduplicating
does not fail loudly; it inflates the graph. The stage tests pin the
deduplication directly for that reason.

See `doc.go` for the full godoc contract.
