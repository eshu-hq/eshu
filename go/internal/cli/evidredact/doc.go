// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package evidredact makes the values of a first-run evidence artifact safe to
// write to disk or paste into a support thread.
//
// It handles four carriers, because a credential reaches an operator artifact
// through any of them and closing one at a time is how this leaked repeatedly:
//
//   - A structured endpoint field. Endpoint removes an embedded "user:password@"
//     userinfo, the value of every credential-named query parameter, and the
//     whole fragment.
//   - A structured filesystem target. Path keeps only the final element of an
//     absolute path, so a home directory cannot spell out a username.
//   - An absolute URL COMPOSED into free-form text — a summary, a hint, a
//     recovery step, an error cause. Text finds it wherever it sits and puts it
//     through Endpoint, so a composed string and the field it was built from
//     never disagree.
//   - A bare "key=value" or "key: value" pair in free-form text with no URL
//     around it. Text scans the text between the URLs with urlredact.FreeText,
//     the same walk internal/reportbundle runs over a support bundle.
//
// The fourth carrier is the one the first three kept hiding. Each earlier round
// closed one carrier and left the next open, and every time the fix looked
// complete because the reported case stopped reproducing. A 401 body carrying
// "api_key=<credential>" is URL-free text, so it went past a walk that only
// redacted URLs — into both the Markdown and the JSON artifact, under an
// endpoint field one line above that was correctly redacted.
//
// Nothing here judges what a VALUE looks like. There is no entropy check and no
// secret-pattern list. Every rule is structural: it asks
// collector.IsSensitiveKeyName about the name to the left of a separator, or it
// acts on a part of a URL whose position is defined by the URL grammar. The
// limits that follow from that are stated on Text and on
// urlredact.FreeText, and they are real: a credential in a URL path segment,
// under a parameter name the predicate does not match, or written as bare prose
// with no key beside it, all survive.
//
// Everything the package returns is a fixed point. An evidence artifact is
// re-rendered from a saved envelope by `eshu first-run report`, so a second pass
// over already-scrubbed text has to find nothing new. TestScrubbedTextIsAFixedPoint
// pins that over the package's own corpus.
//
// The package sits under internal/cli because its endpoint fallback needs
// mcpsetup.RedactToken and its only consumer is the CLI. The mechanism it shares
// with internal/reportbundle lives one layer down in internal/urlredact, which
// depends on nothing but the standard library and the collector name predicate,
// so both walks read one definition of where a credential pair begins and ends.
package evidredact
