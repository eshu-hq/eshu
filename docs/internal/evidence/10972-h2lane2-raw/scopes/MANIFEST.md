# Scope sets (deduplicated record)

The eight scope dumps committed earlier were 5.7MB with 5 byte-identical
copies; per owner direction the dumps were removed and only this record
kept. To re-derive any set, run the census queries in `../statements/runs/`
(`census_member_candidates.sql`, `census_mcv.sql`, `census_membervals.sql`)
against the QA primary and follow the note's scope-screening method.

| md5 | bytes | names that shared these exact bytes |
|---|---|---|
| d50fe92f8a4c27114b4104903751994b | 417513 | x_shipped_pmwg, x_shipped_42043, x_ranged_pmwg, x_ranged_42043, x_red_drop2 |
| c2e94ecaef25b11572a08bb7277cb316 | 394897 | x_shipped_7r86ref, x_ranged_7r86c |
| fc28079b7dead55f1c0cb63fe1ec29fc | 394848 | x_red_keepfirst (242 scopes: the keep-first RED proof) |
| d41d8cd98f00b204e9800998ecf8427e | 0 | x_red_notokens (empty: token-drop RED proof) |

Byte-equality across shipped/routed pairs (258/258/243) was verified from
these files and recorded in the review verdict before removal.
