# #6590: OCI registry-truth key batches are chunked at the recorded bound

## What was wrong

`go/internal/queryplan/testdata/query-source-coverage.yaml` registers the three
OCI registry-truth fetchers in `go/internal/query/impact/trace_deployment_oci.go`
as `keyed_support` / `bounded_key_batch` with `max_keys: 250`. Nothing in the
code enforced that number. The key set is every distinct image reference found
across the traced workloads, so 50 workloads with 6 images each already produce
300 keys. The fetchers sent the whole set as one `IN $digests` /
`IN $image_refs` / `IN $repository_uids` list, which made the recorded bound
false for any trace above 250 distinct images.

## Where the bound belongs

The keys come from `collectContainerImages` in the YAML parser. Capping there
would drop facts at ingest. Truncating the key list in the query would silently
lose registry truth for the images past the cap. Both hide data. The fix instead
splits the key list on the query side into statements of at most
`ociMaxKeysPerStatement` (250) keys and merges the results, so every key is
still queried exactly once and `max_keys: 250` becomes true by construction.

Each fetcher keeps its own `reader.Run` call with unchanged Cypher text; only
the bound parameter value differs per batch. `ociKeyBatches` is a pure splitter
with no graph access, so the query-source coverage registry still attributes
each callsite to the function that runs it.

## Proof

- RED (before the fix): `TestFetchOCIImageRegistryTruthBatchesDigestKeysWithinRecordedBound`
  and `TestFetchOCIImageRegistryTruthBatchesTagRefsWithinRecordedBound` feed 300
  distinct keys and failed with a statement carrying 300 keys, want <= 250.
- GREEN (after): both pass; each asserts every statement carries at most 250
  keys and every input key is queried exactly once.
- `go test ./internal/query/impact/ -count=1`: ok.
- `go test ./internal/queryplan/ -count=1`: ok after re-recording the three
  `source_sha256` values for `fetchOCIImageTagRows`, `fetchOCIImagesByDigest`
  and `fetchOCIRepositoriesByUID`. The class, key bound and result bounds are
  unchanged.
- `go vet ./internal/query/impact/`: clean.

No-Regression Evidence: for a trace with at most 250 distinct image keys, each
fetcher issues exactly the same single statement with the same Cypher text and
the same parameter value as before, so the common case does no extra work. Above
250 keys it issues ceil(n/250) statements of the same indexed `IN` shape instead
of one oversized statement; total keys looked up are identical (n), and each
statement stays inside the bound the query-plan registry already recorded. No
Cypher text, index, or result projection changed. This was not measured against
a live backend because the per-key work and statement shape are unchanged; the
unit tests above prove the key partition.

No-Observability-Change: the fetchers emit no new or changed metrics, spans, or
logs. The existing trace-deployment handler telemetry covers the path as before.

## Related finding, not changed here

`enrichBlastRadiusTiers` (`go/internal/query/impact/impact_blast_radius.go`)
also sends an unchunked key list, but its key set is bounded upstream by the
response limit. Its consumer keeps the last tier row per repository and the tier
read has no `ORDER BY`, so if a repository ever carried more than one tier the
result would be nondeterministic. Today nothing writes `Tier` nodes (only the
uniqueness constraint in `go/internal/graph/schema_tables.go` exists), so the
case cannot occur yet. The first `Tier` writer should enforce one tier per
repository or the read should gain an ordering.
