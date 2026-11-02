// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"fmt"
	"net/url"
	"strings"
)

// parseClaimPlaneURL validates the claim-plane base URL. It accepts only an
// http or https scheme, a non-empty host, and an empty or "/" path, because
// the client appends /workflow.v1/<Operation>. User info, query strings, and
// fragments are rejected. Errors never echo the raw value: it may carry a
// credential the operator pasted by mistake.
func parseClaimPlaneURL(raw string) (*url.URL, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not a valid URL", EnvClaimPlaneURL)
	}
	switch {
	case parsed.Scheme != "http" && parsed.Scheme != "https":
		return nil, fmt.Errorf("%s must use the http or https scheme", EnvClaimPlaneURL)
	case parsed.User != nil:
		return nil, fmt.Errorf("%s must not carry credentials (user info); the claim plane takes none", EnvClaimPlaneURL)
	case parsed.Host == "" || parsed.Hostname() == "":
		return nil, fmt.Errorf("%s must name a host", EnvClaimPlaneURL)
	case parsed.RawQuery != "" || parsed.ForceQuery:
		return nil, fmt.Errorf("%s must not carry a query string", EnvClaimPlaneURL)
	case parsed.Fragment != "" || strings.Contains(raw, "#"):
		return nil, fmt.Errorf("%s must not carry a fragment", EnvClaimPlaneURL)
	case parsed.Path != "" && parsed.Path != "/":
		return nil, fmt.Errorf("%s must be a base URL with no path", EnvClaimPlaneURL)
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}, nil
}
