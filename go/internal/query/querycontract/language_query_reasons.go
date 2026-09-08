// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

// NoBackendReadSourceBackend is the source_backend value a route reports on
// the empty page a scoped caller with no repository grants gets. It is the
// wire spelling of TruthBasisNoBackendRead and matches it exactly, so the two
// fields on such a page say the same thing. Until #6544 this constant was the
// "unavailable" sentinel, reused outside its documented meaning because
// neither vocabulary had a member for a page produced without a read;
// "unavailable" is once again only the default arm's defensive fallback for a
// basis a route does not recognize. The implementation moved from root's
// language_query_reasons.go for #6060 so a handler-family subpackage can
// report the same grantless source_backend without importing root.
const NoBackendReadSourceBackend = "no_backend_read"

// ReasonEmptyGrantNoBackendRead describes the empty page a scoped caller with
// no repository grants gets. It does NOT hide the grantless case from the
// caller: this string is serialized in the truth envelope's reason, so a
// grantless caller can tell its empty page from a granted search that matched
// nothing. What stays unprobeable is the INDEX: neither answer says whether
// any repository, entity or row exists, because no backend was read to find
// out. The implementation moved from root's language_queries.go for #6060 so
// a handler-family subpackage can report the same grantless reason without
// importing root.
const ReasonEmptyGrantNoBackendRead = "the caller's grant admits no repository, so no backend was read"
