# 9594: NuGet Unicode package-ID comparison — registry behavior record

Date: 2026-11-07. Issue: eshu-hq/eshu#9594 (follow-up to #9584, epic #9209).
Question: how does nuget.org compare a Unicode-letter package ID registered
next to its case variant, and does Eshu's ASCII-only fold agree with it?

## Official source

The [nuspec reference](https://learn.microsoft.com/en-us/nuget/reference/nuspec)
defines `id` as "the case-insensitive package identifier, which must be unique
across nuget.org or whatever gallery the package resides in". ASCII case
variants cannot coexist as separate packages (registry read below); for
non-ASCII case pairs no probe exists, so rejection is assumed, not confirmed.

## Client comparer (pinned)

`NuGet/NuGet.Client`,
`src/NuGet.Core/NuGet.Packaging/Core/comparers/PackageIdentityComparer.cs`
at `d4425db531b7bd34d4d7cb19a7e9c65fbcf4705b` (lines verified 2026-11-07):

- `Equals` (L70-L85): `StringComparer.OrdinalIgnoreCase.Equals(x.Id, y.Id)` (L84).
- `GetHashCode` (L90-L101): `combiner.AddObject(obj.Id,
  StringComparer.OrdinalIgnoreCase)` (L99).
- `Compare` (L108-L125): `StringComparer.OrdinalIgnoreCase.Compare(x.Id, y.Id)`
  (L125).

Permalink:
<https://github.com/NuGet/NuGet.Client/blob/d4425db531b7bd34d4d7cb19a7e9c65fbcf4705b/src/NuGet.Core/NuGet.Packaging/Core/comparers/PackageIdentityComparer.cs#L70-L125>

## Server storage and validation (pinned)

`NuGet/NuGetGallery` (lines verified 2026-11-07):

- `src/Catalog/FlatContainerPackagePathProvider.cs` at `0e80f87`
  (`0e80f87628349207cdcaf55358491f8a6f1ca276`): `GetPackagePath` (L18)
  stores the blob under `id.ToLowerInvariant()` (L33); `GetIconPath` (L40-L45)
  does the same (L60).
  Permalink:
  <https://github.com/NuGet/NuGetGallery/blob/0e80f87628349207cdcaf55358491f8a6f1ca276/src/Catalog/FlatContainerPackagePathProvider.cs#L18-L60>
- `src/NuGetGallery.Core/Packaging/PackageIdValidator.cs` at `2f271fb`
  (`2f271fb8651906ecd841d0a658f9afc670e7021d`): new IDs must match
  `^[A-Za-z0-9_]+([.-][A-Za-z0-9_]+)*$` (L13-L15); reads of legacy IDs use
  `^\w+([.-]\w+)*$` case-insensitively (L17-L19).
  Permalink:
  <https://github.com/NuGet/NuGetGallery/blob/2f271fb8651906ecd841d0a658f9afc670e7021d/src/NuGetGallery.Core/Packaging/PackageIdValidator.cs#L13-L19>

## Recorded registry read (2026-11-07T17:57Z, api.nuget.org)

Method: plain HTTPS GETs against the public v3 endpoints, no auth.

| Query | Result |
|---|---|
| `GET https://api.nuget.org/v3-flatcontainer/newtonsoft.json/index.json` | HTTP 200, version list returned |
| `GET https://api.nuget.org/v3-flatcontainer/Newtonsoft.Json/index.json` | HTTP 404 `BlobNotFound` — blobs are keyed under `ToLowerInvariant`, matching `GetPackagePath` |
| `GET https://azuresearch-usnc.nuget.org/query?q=packageid:newtonsoft.json&take=1` | HTTP 200, `totalHits: 1`, `id: Newtonsoft.Json` |
| `GET https://azuresearch-usnc.nuget.org/query?q=packageid:Newtonsoft.Json&take=1` | HTTP 200, `totalHits: 1`, `id: Newtonsoft.Json` — same record |

The comparison layer answers both ASCII case variants with one record while
storage serves only the lowercased blob.

## Unicode mechanics (unchanged from #9584)

- U+0130 (dotted capital I) uppercases to `0049 0307`, so it stays apart from
  `I` under `OrdinalIgnoreCase` too: Eshu and the registry agree, no false
  negative. Source: `UnicodeData.txt` (`0130` maps up to `0049 0307`).
- U+212A (Kelvin sign) uppercases to `004B`, so the registry equates a
  Kelvin-spelled legacy ID with its ASCII twin while Eshu keeps them apart:
  an accepted, legacy-only false negative (new IDs cannot hold non-ASCII
  letters per `IdRegex`). Source: `UnicodeData.txt` (`212A` maps down to
  `006B`).

A live Unicode next-to-case-variant probe is not possible: new registrations
are ASCII-only, and no public legacy ID exercising that pair is known. The
combination above — uniqueness enforced case-insensitively (doc), comparison
via `OrdinalIgnoreCase` over the whole ID (client source), storage under
`ToLowerInvariant` (server source), and the search/flat-container read — is
the recorded substitute.

## Conclusion

Confirmed 2026-11-07: keep Eshu's ASCII-only fold for NuGet, no per-ecosystem
Unicode fold. Decision recorded in `go/internal/packageidentity/README.md`,
pinned by `TestNuGetUnicodeFoldContractKeepsAsciiOnly`.
