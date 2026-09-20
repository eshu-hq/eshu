# gitcontent

## Purpose

`go/internal/collector/git/content` owns the durable fact envelopes the git
collector emits for content: the per-file content fact, the per-entity
content-entity fact, the per-generation repository fact, and the per-file file
fact (with the fingerprint wall-clock observation collapsed to zero so durable
facts stay byte-deterministic).

## Where this fits

```
sync -> discover -> parse -> emit facts -> enqueue -> reducer -> projection -> query
                                    ^
                              this package
```

`gitrepo` drives the repository snapshot and the fact stream. It calls into
this package during emission; this package never calls back into `gitrepo`.
Anything both sides need lives in
`go/internal/collector/gitrepo/gitmodel`.

## Exported surface

- `ContentFactEnvelope` — durable content fact for one snapshot file.
- `ContentEntityFactEnvelope` — durable content-entity fact for one parsed
  entity, including parser `entity_metadata` (fingerprint hashes and sketches).
- `RepositoryFactEnvelope` — durable repository fact for one generation. The
  caller passes the precomputed default branch and ref payload; ref selection
  stays in `gitrepo` with the `GitRef` type.
- `FileFactEnvelope` — durable file fact for one parsed file.

## Notes

Stable keys keep re-emission idempotent: `content:<repo>:<path>`,
`content_entity:<uid>`, `repository:<repo>`, `file:<repo>:<path>`. Changing
what is emitted — new fact kind, changed payload shape, different counts —
changes projected truth, so it needs the cassettes and the B-12 snapshot
updated in the same change.
