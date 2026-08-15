// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package packageruntime

import (
	"net/url"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/urlredact"
)

func safeSourceURI(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" {
		return ""
	}
	if parsed.Host == "" && urlredact.CarriesUserinfo(trimmed) {
		// Not hierarchical, so User can be nil with a credential in plain
		// sight (`svc:SECRET@host/x` keeps it in Opaque, which String()
		// round-trips verbatim). A purl's "@" sits after the first "/", so
		// purls keep flowing; only an authority-shaped credential drops.
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.ForceQuery = false
	parsed.Fragment = ""
	return parsed.String()
}
