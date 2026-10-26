# 8820 research: end-of-life enrichment for declared runtime versions

Recommendation: vendor an offline endoflife.date snapshot for the eight
mapped runtimes, answer with its age on every read, and report
`eol_status: unknown` whenever data is missing. No network at query time.

## Source

- Data: [endoflife.date](https://endoflife.date) per-product JSON API,
  `https://endoflife.date/api/<slug>.json`, generated from the
  [endoflife-date/endoflife.date](https://github.com/endoflife-date/endoflife.date)
  repository (485 products as of 2026-10-26, `GET /api/all.json`).
- Record shape (nodejs shown; keys vary by product — `support`,
  `extendedSupport`, and `lts` may be absent, `eol` may be `false` for
  rolling releases):
  `cycle, releaseDate, eol, latest, latestReleaseDate, support, lts`.
- Refresh path: a scheduled job fetches the mapped product files, writes
  them plus a `snapshot_date` (UTC day of fetch) into the vendored
  snapshot directory, and opens the update as a normal PR. Query code
  reads only the vendored files, so air-gapped and offline deployments
  behave identically to connected ones, modulo staleness.

## Runtime mapping

Eshu canonical runtimes (`go/internal/collector/repo/runtime/declaration/parse.go`,
`fileRuntime` plus open tool tokens) map to endoflife.date slugs:

| Eshu runtime | slug | bytes (2026-10-26) |
|---|---|---|
| node | nodejs | 4,764 |
| go | go | 2,342 |
| python | python | 2,922 |
| ruby | ruby | 1,968 |
| php | php | 2,664 |
| rust | rust | 9,101 |
| terraform | terraform | 2,112 |
| dotnet (open tool token: `.tool-versions`, `.sdkmanrc`) | dotnet | 1,966 |

Total: ~27 KB. Vendor only these eight; the remaining 477 products are
out of scope for declared-version questions.

- `java`: no plain `java` slug exists (only vendor builds such as
  `amazon-corretto`, `eclipse-temurin`); the mapping is ambiguous, so
  `java` declarations report `unknown`.
- `swift`: no `swift` slug; reports `unknown`.
- Any unmapped or newly seen tool token reports `unknown`. A missing
  cycle within a mapped product also reports `unknown`. Missing data is
  never read as supported.

## Answer contract

- Every enriched answer carries the snapshot's `snapshot_date` and its
  age in days.
- Every declaration resolves to one of `supported`, `eol`,
  `support-ending` (past `support`, before `eol`), or `unknown`.
- `unknown` covers: unmapped runtime, missing product file, missing
  cycle, unparseable spec (alias/range/unresolved stay as they are).

## License

The dataset repository is MIT licensed:

```text
Copyright 2020 endoflife.date contributors

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, ...
```

(Full text: `LICENSE` in endoflife-date/endoflife.date, fetched 2026-10-26.)
Carry the attribution under a new "Third-party data" section in
`docs/public/license.md` naming endoflife.date, the vendored snapshot
date, and the MIT copyright line above.

## Follow-up

Implementation is tracked separately (filed from this research). It owns:
the vendored snapshot directory plus refresh job, the normalizer, the
read-path join against `runtime_declaration.version`, the age/unknown
answer contract, the licence section, and the staleness signal.
