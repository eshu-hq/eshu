# Dockerfile Parser Helpers

## Purpose

This package owns Dockerfile parser helpers that do not need parent parser
payload helpers. It extracts runtime metadata such as stages, exposed ports,
arguments, environment variables, labels, build platforms, and runtime commands
from Dockerfile source text.

## Ownership boundary

The package is responsible for typed Dockerfile runtime evidence. The parent
parser package still owns file I/O, parser registry dispatch, payload assembly,
and the exported compatibility wrapper used by query and relationship code.

## Exported surface

The godoc contract is in doc.go. Current exports are Metadata, Stage, Arg, Env,
Port, Label, Install, UnparsedCommand, RuntimeMetadata, and Metadata.Map. The Stage type carries
BaseRefTemplate, UnresolvedArgs, ArgDefaults, ArgDefaultsTruncated, and
ArgDefaultsOmitted for the FROM substitution evidence above.

## Dependencies

This package imports only the Go standard library. It must not import the
parent parser package, collector packages, graph storage, or reducer code.

## Telemetry

This package emits no telemetry. Parse timing remains owned by the parent
parser engine.

## Gotchas / invariants

RuntimeMetadata keeps bucket ordering deterministic by sorting rows by name,
matching the old parent parser payload behavior.

RuntimeMetadata follows Dockerfile command-line token rules for the metadata it
models: quoted and escaped `ENV` and `LABEL` values keep spaces, legacy
`ENV key value` lines emit a single environment row, multi-argument `ARG`
instructions emit one row per argument, `FROM --platform=...` records stage
platform metadata, registry hosts with ports do not get split as image tags,
and the Dockerfile `# escape=` parser directive controls line continuation plus
token escaping when it appears before any non-directive comment, empty line, or
instruction. The tokenization helpers live in `tokens.go` so `metadata.go`
stays below the package line cap.

### FROM base-image ARG substitution

`base_ref.go` expands ARG references in a `FROM` image reference, following the
Dockerfile rule for how `ARG` and `FROM` interact:

- Only ARGs declared before the first `FROM` are visible to a `FROM` line. An
  ARG declared inside a stage, including after the stage's own `FROM`, is never
  visible to that stage's `FROM` or to any later one.
- `$VAR`, `${VAR}`, `${VAR:-word}`, `${VAR-word}`, `${VAR:+word}`, and
  `${VAR+word}` are expanded. A global ARG default may itself reference an
  earlier global ARG. The last global declaration of a name wins. The
  expansion runs before the reference is split into image and tag, so
  `ARG BASE=golang:1.24` with `FROM $BASE` yields image `golang`, tag `1.24`.
  Digest pins, `--platform`, `AS`, `FROM scratch`, and `FROM <earlier stage>`
  are unchanged. The `--platform` value is kept as written.
- A reference stays unresolved when any variable has no value known at parse
  time: an ARG without a default, an undeclared variable used without an
  operator, an ARG whose default is itself unresolved, a predefined platform
  ARG such as `TARGETARCH` (the parser cannot know the target platform, so it
  reports these as unresolved on purpose, even when a default is declared),
  or an expression such as `${V%%.*}`, `${V:?msg}`, or `$(cmd)`. An ARG
  declared without a default is treated as a possible build argument, so `${V:-lts}` stays
  unresolved when `ARG V` is declared and resolves to `lts` only when `V` is
  never declared.
- When resolved, `base_image` and `base_tag` hold the expanded value, and
  `base_ref_template` holds the reference as written. When unresolved,
  `base_image` and `base_tag` keep the reference as written (placeholder
  included), `base_ref_unresolved_args` lists the variable names (an
  expression such as `${V:?msg}` or `${V%%.*}` lists `V`, never the expression
  text, so `V`'s declared default still attaches as evidence), and
  `base_ref_arg_defaults` holds any default seen for them as evidence only
  (for example a stage-local ARG default the `FROM` could not see). All three
  keys are omitted for a `FROM` with no variable, so existing payloads are
  byte-identical. They ride in the open attributes of the typed stage contract.
  The default evidence is bounded (see the next bullets); the stored text is
  never used to resolve a reference.
- A construct with no variable name cannot be listed by name, and its text is
  never copied into `base_ref_unresolved_args` (it is repository-controlled and
  can be 4096 bytes). The list holds one fixed marker instead: `$(` for a
  command substitution, `${` for `${}`, `${1}` or an unterminated `${`, `$` for
  a bare dollar, and `<over-bound>` for a reference rejected for length or
  nesting depth. Each marker holds a character a variable name cannot, so a
  marker never collides with a name. A reader that wants variable names skips
  entries that do not start with a letter or underscore.
- An unaliased stage is named after its expanded image.
- A resolved value is the Dockerfile's own default. A CI `--build-arg` can
  override it at build time, and the parser cannot see that.
- Two unaliased stages whose bases expand to the same image share a stage
  `Name`, so ports and env matched by stage name attribute to both. This
  already happened for `FROM node` written twice; ARG expansion widens it.
- Expansion is bounded. A Dockerfile is untrusted repository content. A
  reference over 4096 bytes, or nested deeper than 64 `${` levels, is rejected
  in one linear pass before any expansion. An expansion that produces more than
  4096 bytes is unresolved, however the bytes are built up: ARGs that each
  repeat the previous one, or that each pad the previous value with literal
  text, both hit the cap. The cap is checked before each write, so the output
  never passes 4096 bytes, and an ARG default that expands past it is never
  stored as a known value and later `FROM` lines cannot read it. In every case
  the reference stays unresolved. All nesting levels of one reference write into
  a single output buffer, so no level copies an inner level's result outward.
  Allocation for one reference is bounded by the reference length (at most
  4096 bytes), not by the output cap, because the name lists grow once per `$`.
  A 4094-byte reference of bare `$U` allocates about 250 KB, and a 631-byte
  nested line about 9.1 KB. Scan work is bounded by the output cap and 64
  times the reference length, whatever it nests.
  Not retaining the value of an unknown default is hygiene: `lookup` already
  ignores a global that is not known, so correctness does not depend on it.
- The ARG default evidence is bounded too. A raw default is repeated on every
  `FROM` stage that reads it, so an unbounded copy made the payload grow with
  ARGs times `FROM` lines. Each stored default is cut to 256 bytes (on a rune
  boundary) and a stage keeps at most 16 defaults (the first 16 names in sorted
  order, then stage-local ARGs in declaration order). The cut is visible:
  `base_ref_arg_defaults_truncated` lists the names whose stored text is a
  prefix, and `base_ref_arg_defaults_omitted: true` says at least one default
  was left out for the per-stage maximum (`base_ref_unresolved_args` still lists
  every name). Both keys are absent when nothing was cut, so ordinary payloads
  are unchanged. A stage row is self-contained evidence, so a default is not
  deduplicated across stages; the per-stage cap bounds the repeat instead. The
  bound is 16 values of 256 bytes plus their names. Names are not capped on
  their own, but they come from one reference of at most 4096 bytes, so the
  `base_ref_arg_defaults` evidence of a stage is at most about 8 KB. The
  stage row also carries `base_ref_arg_defaults_truncated` (at most 16 names)
  and `base_ref_unresolved_args` (at most the name bytes of one 4096-byte
  reference), which ride on the row on top of that figure.

No-Regression Evidence: #8810. The reference bound adds one linear pass over
each reference that contains `$`. The output cap adds one length compare after
each expansion. The evidence cap adds one length compare per stored default.
`go test -run xxx -bench BaseRefExpansion/ordinary -benchmem -benchtime=500000x`,
old and new interleaved on one loaded host (load average about 32): the ordinary
`ARG` plus `FROM` Dockerfile took 2.3 to 6.3 us per Dockerfile depending on host
load, with old and new within noise of each other in every pair, and 33
allocs/op old, 36 new (5504 B/op old, 6136 B/op new: the expander state and
the three added `Stage` fields). That is no measurable change in time.
Differential: `RuntimeMetadata(...).Map()`
JSON for the 23 tracked Dockerfile inputs (22 files named `Dockerfile`,
`Dockerfile.<ext>` or `*.Dockerfile`, plus the `Dockerfile.j2` template) is
byte-identical between the unbounded and the bounded expander. Against main, 5
of them change by design because they have an ARG in `FROM` (`Dockerfile`,
`examples/collector-extensions/pagerduty/Dockerfile`,
`examples/collector-extensions/scorecard/Dockerfile`,
`examples/collector-extensions/scorecard/oci.worker.Dockerfile`,
`scripts/ci/tools.Dockerfile`): they gain `base_ref_template` and an expanded
base.
Hostile input, re-measured together on one host for the review that found the
figures disagreed (`-benchtime=3x -count=5` for one line; one run for the 200
lines with the bound disabled; a scratch copy of the pre-shared-buffer code
with `exceedsExpansionBounds` forced to `false`): a balanced 60 KB line of
nested `${A:-` blocks took 442-454
ms/op and 835.8 MB/op with the reference bound disabled and 0.39 ms/op and 0.54
MB/op with it. 200 such lines in one Dockerfile took 48.8 s and 167 GB
allocated with the bound disabled. With the bound, the same 200 lines
(`hostileNestedFrom(60_000)` repeated 200 times through `RuntimeMetadata`,
`-benchtime=3x -count=5`, load average about 52) took 93-101 ms/op and 87.7
MB/op; the time is host-dependent (a reviewer's rerun on a loaded host took
0.145-0.54 s). Earlier figures in
other notes (366 ms, 461-887 ms, 59.8 s, 68.4 s) came from different load
levels and are superseded by this set. A padded ARG chain (100 ARGs of
about 4000 literal bytes each repeating the previous one, then 1000
`FROM i:${A100}` lines, 416 KB of input) took 563-576 ms/op and 426.6 MB/op
before the output cap, and left 400 MB of base tags across the 1000 stages. It
takes 7.8-13.1 ms/op and 5.0 MB/op after, and every stage stays unresolved with
a 7-byte tag (`${A100}`).
ARG default evidence, marshaled `json.Marshal(RuntimeMetadata(src).Map())`
(`TestBaseRefArgDefaultEvidencePayloadStaysBounded`; 200 ARGs of
`ARG Un=${TARGETARCH}` plus 4000 bytes, one `FROM i:$U0$U1...$U199` line per
stage):

| Shape | Input | Payload before | Payload after |
| --- | --- | --- | --- |
| 200 ARGs, 100 FROM lines | 894,290 B | 81,589,217 B | 1,583,117 B |
| 200 ARGs, 1000 FROM lines | 1,702,490 B | 808,575,218 B | 8,514,218 B |
| 1000 ARGs, 800 refs per FROM, 1000 FROM lines | 7,920,890 B | 3,235,828,818 B | 21,967,818 B |

Wall time to parse and marshal the second row: 1.53 s before, 0.08-0.12 s after;
the third: 5.56 s before, 0.27-0.32 s after (a loaded host, two runs). The
remaining payload is the input carried through: every ARG row holds its
default once, and each stage row holds its own
`FROM` text and name list. The evidence a stage adds is at most 16 values of
256 bytes plus their names, about 8 KB (4251 B measured with short names).
`TestBaseRefOversizedNestedReferenceIsUnresolvedAndCheap`,
`TestBaseRefManyNestedReferencesStayBounded`,
`TestBaseRefPaddedArgChainIsUnresolved`, and
`TestBaseRefPaddedArgChainDoesNotAmplifyAcrossStages` pin the expansion cases.

No-Regression Evidence: #8810, shared-buffer expansion (review round four).
A reference inside both caps could still cost O(depth^2 x 4096) bytes: each
nesting level built its own output and the whole result was copied outward at
every level. `TestBaseRefNestedPaddedReferenceAllocationIsBounded` pins the
shape the review measured (`ARG K` of 4000 bytes, then 1000 lines of
`FROM img:${U:-${K}${U:-${K}...x}}}` 63 deep, 631 bytes per line, 645,007
bytes of input) with a 50 MB ceiling on `runtime.MemStats` `TotalAlloc`. It
failed before the fix (8,870,755,872 bytes allocated) and passes after; with
the nested-copy behavior put back it fails again (341,547,928 bytes).
`go test -run xxx -bench 'BaseRef' -benchmem -count=5`, same quiet-as-possible
host both times (the host was loaded, load average 12-20 on 16 cores, so read
ns/op as an upper bound; B/op and allocs/op are stable):

| Benchmark | Before ns/op | Before B/op | After ns/op | After B/op |
| --- | --- | --- | --- | --- |
| `BaseRefNestedPadded` (1000 lines, 63 deep) | 1.26-1.38 s | 8.874 GB (332,201-332,328 allocs) | 7.4-12.5 ms | 9.11 MB (21,060 allocs) |
| `BaseRefExpansion/hostile_60KB` | 0.88-1.08 ms | 543,977-543,990 | 0.39 ms | 544,034-544,036 |
| `BaseRefExpansion/padded_chain_100_args_1000_froms` | 4.8-9.5 ms | 5,154,162-5,154,374 | 5.1-5.5 ms | 5,145,311-5,145,334 |
| `BaseRefExpansion/ordinary` | 2.4-4.8 us | 5504 (33 allocs) | 2.3-2.5 us | 6136 (36 allocs) |

The ordinary Dockerfile gains three allocations and 632 bytes for the
expander state; its time is unchanged. `RuntimeMetadata` also runs at read time
(repository runtime artifacts and the Dockerfile relationship read), so this
bound is paid on reads, not only at ingest.

No-Observability-Change: the parser emits no telemetry of its own. Parse
timing stays on the parent parser engine's existing per-file duration. A
rejected reference shows up as `base_ref_unresolved_args` on the stage.

Known limit: the tokenizer drops backslash escapes and quotes before
expansion, so an escaped `\$` or single-quoted `$` is treated as a variable.

Metadata.Map keeps the legacy `map[string]any` payload shape because query and
relationship packages still consume `parser.ExtractDockerfileRuntimeMetadata`.

## Related docs

- docs/public/languages/support-maturity.md

### RUN and ADD install declarations (#9303)

`RuntimeMetadata` records recognized source declarations in `Installs` and in
`dockerfile_installs` payload rows (`manager`, `name`, optional pinned `version`,
`line_number`, `stage`). `a2enmod` and `docker-php-ext-*` use the same row
contract; PHP configuration readers consume these rows instead of parsing
Dockerfile commands again. An install row says only that source declared an
install, not that the built image contains it.

The bounded scanner accepts package managers, PHP extension commands, and
HTTP(S) downloads from RUN or ADD without executing shell. A global ARG can
expand a FROM reference, but becomes available to RUN only after an ARG of the
same name is declared inside that stage. An unrelated FROM starts with no
install variables; FROM a prior named stage inherits its ARG and ENV values.
ENV wins when both provide the same name. Values remain source defaults, since
build-time overrides are unknown to the parser.

RUN shell comments end at an unquoted `#` that begins a word. The parser
consumes Dockerfile `RUN --mount`, `--network`, `--security`, and `--device`
flags before reading the shell command. Recognized package-manager options
before or after the action consume their values before package detection; an
unknown option rejects the command. A simple executable RUN heredoc can
provide install declarations. Shell data or control-flow heredocs and COPY/ADD
data bodies do not become RUN declarations. ADD accepts recognized leading
flags before URL sources. Unsupported commands are fail-closed:
`dockerfile_unparsed` records a line and reason without keeping command text.

`dockerfile_dropped_credentials` counts rejected credential-bearing option
values, names, and URLs, including encoded credential markers in URL paths.
Signed download query keys such as AWS and Google Cloud `Signature` or
`Credential` fields and Azure `sig` are rejected after bounded percent decoding;
malformed query escapes are rejected as unsafe. Ordinary query keys are
accepted, and query values are never kept in install names. The same guard
counts signed URLs consumed as install option values or package words.
Sensitive ARG or ENV names and their indirect aliases remain tainted when
expanded into an install name or download URL; these references are counted
and excluded from install evidence.
Only a credential-free download host and path can be retained. A `curl` or
`wget` URL supplied entirely through a known ARG or ENV is expanded before
classification. Repository
artifact readers expose the same rows per Dockerfile stage. Parent parser
file-parse timing remains the operator signal; this package adds no metric.

Accuracy Evidence: the parser retains source order for install rows and leaves
existing stage, port, ARG, ENV and label buckets unchanged. Focused parser and
collector fact tests cover install forms, stage binding, heredoc boundaries,
option values, and credential drops through persisted install entities and
content reference names.

Performance Evidence: two quiet-host base/candidate rounds used the identical
`BenchmarkInstallEvidenceStageScope` harness and
`GOMAXPROCS=4 go test ./internal/parser/dockerfile -run '^$' -bench
'^BenchmarkInstallEvidenceStageScope$' -benchtime=100x -count=5 -benchmem`.
Median ordinary parse cost moved from 4.602-4.713 to 5.789-5.892
microseconds (+25-26%, about 1.2 microseconds). One inherited lookup across
1,000 stages moved from 1.438-1.488 to 1.865-1.880 ms (+25-31%). A
1,001-stage Dockerfile with 100 final-stage inherited installs moved from
1.516-1.561 to 3.037-3.048 ms: a material 95-101% and about 1.5 ms
increase. Five fixed ecosystem fixture Dockerfiles (seven stages total)
moved from 20.096-20.418 to 23.773-24.039 microseconds per five-file pass:
+16-20%, or 3.36-3.94 microseconds. A guarded fast path was proved first
against the same fixture words and encoded/opaque values; it saved about
1.5 microseconds per five-file pass relative to the prior candidate. These
are paired medians of five samples per side in each of two quiet-host rounds;
raw output is retained in the run evidence. The candidate stayed below the
provisional 50 ms hostile-case investigation bound, but this is not an
end-to-end or production-corpus no-regression claim. The visible files in this
checkout have at most seven stages; this is only a checkout proxy.
The original preimplementation timing investigation expired before a quiet
host was available, so its theory remains unproven. The later paired run
verifies only the implemented parser path. No new metric or span was added;
the parent parser's existing file-parse timing remains the operator signal.
The figures above predate the signed-query guard. Its first paired comparison
found +1.8% and -1.1% on the unchanged five-file fixture corpus. A final
paired comparison of the shared URL classifier found +0.8–0.9% per five-file
pass and +4.2–8.0% on an ordinary pip-index URL microcase, with one extra
allocation in that case. See [signed-query proof](../../../../docs/internal/evidence/9303-dockerfile-signed-query-proof.md)
for the source hashes, query controls, and interpretation.
