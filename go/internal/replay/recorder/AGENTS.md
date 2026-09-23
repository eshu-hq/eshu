# recorder — agent scope

## Owned surface

- `go/internal/replay/recorder/` — `Run` (one-shot record of a collector.Source
  into a canonical cassette) and the envelope→cassette mapping.

## Key invariants

- **Record captures, never synthesizes.** The recorder copies the collector's
  emitted envelopes verbatim — payload included — so a fact's `object_id` is the
  collector's real `facts.StableID` value. Do not rewrite, normalize, or
  recompute payload fields here; that would reintroduce the #3928 drift the
  recorder exists to prevent. The one sanctioned rewrite is identifier
  pseudonymization, and it happens upstream of the recorder, in the
  `recordpseudo.Wrap` source that `Options.Pseudonymize` installs, keyed and
  structure-preserving; the recorder still copies what it is handed.
- **Never write what the gate would refuse.** `recordpseudo.Verify` runs on
  the canonical bytes before `os.WriteFile` for every recording, and a
  refusal writes nothing. Do not move it after the write, make it
  conditional, or let it print a value. `RequirePseudonymization` refuses
  before the source is polled; keep that ordering.
- **Output MUST stay canonical and deterministic.** Always serialize through
  `replay.Canonicalize`. A `record → replay → record` cycle must be
  byte-identical (`TestRecordIsCanonicalAndStable`); never embed a timestamp,
  hostname, or map-ordered field that would churn.
- **Never write an invalid or partial cassette.** Abort on a generation's
  `FactStreamErr`, reject an empty batch, and load the output back through
  `cassette.LoadFile` before returning; remove the file on a load-back failure.
- **Record needs no database.** `Run` performs no commit. Keep it that way: a
  fixture-generation tool must run with only the collector's live credentials.
- **The mapping must track the cassette format.** `toScope`/`toFact` mirror the
  `cassette.Scope`/`cassette.Fact` fields. When the cassette format gains a
  durable field, map it here too.

## Skill routing

- `golang-engineering` for any Go change here.
- `eshu-golden-corpus-rigor` — recorded cassettes are what the B-7 golden-corpus
  gate replays; a recording-shape change ripples to every regenerated cassette.

## Do not

- Recompute or "fix up" payloads during recording.
- Introduce nondeterminism (timestamps, map-order-dependent output).
- Add a durable commit or a database dependency to the record path.
- Log, return or format a raw or pseudonymized value; report paths and counts.
