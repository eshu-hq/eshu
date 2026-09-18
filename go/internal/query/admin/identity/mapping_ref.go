// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package identity

import (
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// isLegacyIDPGroupMappingRef recognizes the old 32-hex MD5 references so a
// delete cannot silently report a no-op after the FIPS-safe SHA-256 transition.
func isLegacyIDPGroupMappingRef(ref string) bool {
	if len(ref) != 32 {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

// isCurrentIDPGroupMappingRef accepts a canonical SHA-256 mapping reference for
// keyset pagination. The same form is returned by the list and create routes.
func isCurrentIDPGroupMappingRef(ref string) bool {
	if len(ref) != 64 || strings.ToLower(ref) != ref {
		return false
	}
	_, err := hex.DecodeString(ref)
	return err == nil
}

// afterRefCursor reads the after_ref keyset cursor from the raw query without
// normalization, as the route contract documents: an absent after_ref is the
// first page; a query string that does not decode, a repeated after_ref, or a
// present value (including an empty one) that is not a current 64-hex
// mapping_ref answers 400 and returns false after writing the error.
func afterRefCursor(w http.ResponseWriter, r *http.Request) (string, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		querycontract.WriteError(w, http.StatusBadRequest, "query string must be URL-encoded")
		return "", false
	}
	refs := values["after_ref"]
	switch {
	case len(refs) > 1:
		querycontract.WriteError(w, http.StatusBadRequest, "after_ref must be supplied at most once")
		return "", false
	case len(refs) == 0:
		return "", true
	case !isCurrentIDPGroupMappingRef(refs[0]):
		querycontract.WriteError(w, http.StatusBadRequest, "after_ref must be a lowercase 64-hex mapping_ref")
		return "", false
	}
	return refs[0], true
}
