# apilib/admin — Agent Instructions

Scope: `go/apilib/admin/` (package `admin`). Definition site for the
proof-read port and handlers (Refs eshu-hq/eshu-api#456).

## Ownership

Core aliases every row/filter type here (`type X =
apilibadmin.X` in `go/internal/query/admin/`); a core store satisfies
`ReadStore` only while these signatures stay identical. Changing a
method signature, row field, filter field, JSON tag, or error text here
is a wire change on both sides: update core's delegation and the
byte-parity tests in the same change.

- Do not add store methods, handlers, or routes beyond the proof reads.
- Do not import anything beyond stdlib (see parent AGENTS.md).

## Failure Answers

Same contract as the parent: nil store 503, bad JSON 400, fixed 500,
client-cancel 499, fence hook first. `WriteTuningReport` replicates the
shared JSON encoding (charset utf-8, `EscapeHTML` off) used by the core
tuning path — pinned by `TestTuningReportMatchesSatelliteBytes`.
