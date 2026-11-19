# CLISupportReadAdmission v0 (core pin record)

Refs eshu-hq/eshu#9669. Wave-5 CLISupport tier-b core pin, per the
arbiter step-2 scope ruling (decisions 2, 6, 7). Minted core-side;
the satellite records this pin in a follow-up. A satellite PR
consuming an unminted pin is invalid. Arbiter review of this pin
precedes satellite termination of the CLI-support legs.

## Shape

Version: `CLISupportReadAdmissionContractVersion = "v0"`
(go/internal/cli/support/read_admission.go).

Exactly 10 re-declared (never aliased) derives, sealed — every one a
bounded pure derivation over caller-supplied bytes:

| # | Method | Mirrors | Serves |
|---|--------|---------|--------|
| 1 | `RenderInvestigationPacket` | `packet.RenderInvestigationPacket` | `POST /api/v0/investigation/packet/render` |
| 2 | `ParseInvestigationPacketFormat` | `packet.ParseInvestigationPacketFormat` | same render leg, format parse |
| 3 | `ServiceStoryInput` | `serviceintel.FromServiceStory` | `POST /api/v0/service/report/compose` story adapter |
| 4 | `SupplyChainSectionInput` | `serviceintel.FromSupplyChainInventory` | same compose leg, supply-chain adapter |
| 5 | `ComposeServiceIntelReport` | `serviceintel.Compose` | same compose leg, report compose |
| 6 | `CheckReportTargetCredentials` | `reportbundle.CheckTargetCredentials` | `POST /api/v0/report/bundle/capture` target screening |
| 7 | `CaptureReportBundle` | `reportbundle.Capture` | same capture leg, bundle capture |
| 8 | `ValidateReportBundle` | `reportbundle.Validate` | `POST /api/v0/report/bundle/validate` |
| 9 | `ParseAnswerEvidence` | `answerquality.ParseEvidence` | `POST /api/v0/answer-quality/score` evidence parse |
| 10 | `ScoreAnswerEvidence` | `answerquality.Score` | same score leg, evidence score |

Route oracle codes (oracle `8a78eb3f48`): `POST
/api/v0/answer-quality/score` (`paths/cli/score.go`:
200/400/413/500), `POST /api/v0/investigation/packet/render`
(`paths/cli/render.go`: 200/400/413/500), `POST
/api/v0/report/bundle/capture` (`paths/cli/capture.go`:
200/400/413/422/500), `POST /api/v0/report/bundle/validate`
(`paths/cli/validate.go`: 200/400/413/500), `POST
/api/v0/service/report/compose` (`paths/cli/compose.go`:
200/400/413/500).

Transport: no constructor on this pin. The `cliSupportStaticDeriver`
adapter delegates each re-declared derive to the static function the
handler legs call today, so removal, rename, or re-signing of any of
the ten fails compilation here. This pin lives in package support
because the handlers live outside internal/query — the ruling's
no-new-tree rule is met, the pin sits beside the handlers it covers,
the same placement the query-tree pins use. Concrete-double-only
proof is rejected.

Enforcement (all in go/internal/cli/support):

- `read_admission_contract_test.go` — frozen method-set test plus
  version pin
  (`TestCLISupportReadAdmissionContractIsVersioned`), witness
  (`cliSupportReadAdmissionWitness` satisfier assertion), seal
  (`TestCLISupportReadAdmissionWireShapesAreSealed` — exactly 10
  derives), and the seeded-drift RED half
  (`TestCLISupportReadAdmissionSeededDriftIsDetected`).

## Satellite consumption

Depend on the shape, not a constructor: serve the five legs through
the existing static derives, the same functions the legs call today.
This is the W5 CLI-support family (all 5 oracle CLISupport rows). A
satellite PR consuming this pin for any wider derive surface is
invalid without arbiter review.

## W8a disclosed-absence posture

All five routes are natively mounted in-tree and their OpenAPI
fragments stay core-side (oracle `paths/cli/score.go`,
`paths/cli/render.go`, `paths/cli/capture.go`,
`paths/cli/validate.go`, `paths/cli/compose.go`, codes above) —
disclosed here, not silently dropped. No `contracts/upstream/*` stub
covers these legs: the wire-shape generator snapshots forwarded relay
spellings only, and no CLISupport leg has one.

## Explicitly OUT

Also OUT (each keeps its own standing until a deliberate pin covers
it):

- The body caps, strict-JSON decode, required-field checks, and the
  400/413/422 mappings (handler behavior, not port shape).
- The credential-screening predicate lives inside the capture derive
  (`CheckReportTargetCredentials`), not as a separate gate: the pin
  freezes the derive that enforces it.

## Rollback

Revert this pin. The seam is unwired — no handler, route, or caller
depends on it yet — so rollback moves no traffic and changes no data.
