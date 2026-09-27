# AGENTS.md — IAM escalation projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../../../AGENTS.md` and `../../../../README.md` for projector-wide invariants.
3. `../../../../intent/AGENTS.md` for the neutral builder contract.
4. `../../../../scope_generation_intents.go` for root-owned assembly order.
5. `../perform/AGENTS.md` for the sibling CAN_PERFORM builder this family
   sits beside (same entity key, same trigger fact kind, overlapping
   `policy_source` predicate, disjoint reducer domains).
6. `go/internal/reducer/iamescalation/README.md` for the extractor, catalog,
   and skip taxonomy the intent feeds.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildIAMEscalationMaterializationReducerIntent` anchors to the earliest
  fact in original input order that qualifies (`FirstOfKindMatching`): an
  `aws_iam_permission` fact whose payload decodes and carries
  `policy_source` `"inline"` or `"attached_managed"`. Either effect
  qualifies. A trust statement (`policy_source == "trust"`) never qualifies
  — that fact kind's trust statements are the sibling `trust` package's
  trigger, not this one's. Do not widen the predicate to accept `"trust"`,
  or turn a decode failure into a returned error; each changes `FactID` or
  drops a valid generation.
- The payload decode goes through this package's own
  `factschema_decode_iam.go` (`decodeIAMEscalationAWSIAMPermission`;
  `sdk/go/factschema` plus `internal/factenvelope` directly), never root's
  classified decode wrapper or a sibling package's decode copy. The wrapper
  is named distinctly from `trust`'s `decodeAWSIAMPermission` and
  `perform`'s `decodeIAMCanPerformAWSIAMPermission` on purpose:
  `scripts/verify-payload-usage-manifest.sh` requires every decode seam under
  `go/internal/projector` to have a globally unique function name (it keys
  seams by bare identifier, not by (package, identifier)). Root imports this
  package to dispatch, so the reverse import cycles.
