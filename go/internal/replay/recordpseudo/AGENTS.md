# recordpseudo — agent scope

## Owned surface

- `go/internal/replay/recordpseudo/` — the record-mode pseudonymization
  engine: `Key`, `Class`/`Policy`, the learning and substitution rules,
  `Wrap`/`Source`/`Report`, and `Verify`.

## Key invariants

- **Never a value.** No function in this package logs, returns, formats or
  panics with a raw or pseudonymized value. `Report` carries counts, paths
  and the key fingerprint; `Verify` errors carry offsets and alternatives.
  A test that prints a value to debug must not be committed.
- **Class-free HMAC input.** The pseudonym material is
  `HMAC(key, "recordpseudo/v1\0" + raw)`; the class shapes only the output.
  Changing the input domain re-keys every recorded corpus and breaks
  cross-cassette joins.
- **Fail closed.** An unlisted key is opaque and reported. Do not add a
  "pass through unknown strings" mode.
- **Shape before readability.** The account form must stay 12 digits, the
  ARN grammar must keep partition/service/region/type token, AWS-issued ids
  keep their prefix, digests are kept, customer image tags are
  pseudonymized. Substitution is structure-aware (ARNs by position,
  regions protected, short or numeric free-text tokens exact-only but
  looked up whole per composite component); a change to it must keep
  `TestShortTokensNeverRewriteStructure`,
  `TestShortWholeNamesAreRewrittenInComposites`,
  `TestNumericNamesAreLearned`, `TestNumericNamesNeverRewriteQualifiers`,
  `TestKeepFieldsIgnoreExactOnlyTokens`,
  `TestAccountsInsideARNPathsArePseudonymized`,
  `TestAWSVocabularyWordsAreNeverLearned`,
  `TestNumericNameAfterTypedColonIsAName` and
  `TestShortNumericTagValuesAreKept` green. The AWS vocabulary
  (`arn_vocabulary.go`) is structural: a learned token equal to one of
  its words would rewrite service and type segments everywhere, so `set`
  refuses it; extend the vocabulary, never bypass the refusal. The reducer
  extractors are
  the judge: `TestAWSCorpusShapePreserved` compares raw and pseudonymized
  rows and must stay at the design's exact numbers.
- **Verify mirrors the gate.** The allow forms in `verify_forms.go` follow
  `scripts/lib/cassette_private_data_pattern.sh`; when the gate's allowlist
  changes, change both and keep `TestVerifyAgreesWithGateOnCommittedCorpus`
  green. The `0000` account form and the `h`-label AWS endpoint form are
  admitted only from `produced`. The endpoint form's region grammar and
  word list are pinned to the gate lib by `TestAWSEndpointFormMirrorsTheGate`
  and to `awsHostServiceLabels` by
  `TestAWSEndpointWordsAreTheKeptServiceLabels`: add a kept host service
  word in all three places.
- **Determinism.** Same key, same input, same bytes. No time, no randomness,
  no map-order-dependent output.

## Skill routing

- `golang-engineering` for any Go change.
- `eshu-golden-corpus-rigor` — the output is what B-7 replays.
- `eshu-correlation-truth` when a class change could alter reducer truth.

## Do not

- Put the class or the field path into the HMAC input.
- Widen a pseudonym shape to a form the gate does not admit.
- Serialize or log the dictionary.
