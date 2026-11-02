// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// parseClaimPlaneURL validates the claim-plane base URL. It accepts only an
// http or https scheme, a non-empty printable-ASCII host, an optional port in
// 1-65535, and an empty or "/" path, because the client appends
// /workflow.v1/<Operation>. User info, query strings, and fragments are
// rejected. Errors never echo the raw value: it may carry a
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
	case !printableASCII(parsed.Hostname()):
		return nil, fmt.Errorf("%s host must be printable ASCII with no whitespace (use the punycode form)", EnvClaimPlaneURL)
	case !validPort(parsed.Port()):
		return nil, fmt.Errorf("%s port must be a number from 1 to 65535", EnvClaimPlaneURL)
	case parsed.RawQuery != "" || parsed.ForceQuery:
		return nil, fmt.Errorf("%s must not carry a query string", EnvClaimPlaneURL)
	case parsed.Fragment != "" || strings.Contains(raw, "#"):
		return nil, fmt.Errorf("%s must not carry a fragment", EnvClaimPlaneURL)
	case parsed.Path != "" && parsed.Path != "/":
		return nil, fmt.Errorf("%s must be a base URL with no path", EnvClaimPlaneURL)
	}
	return &url.URL{Scheme: parsed.Scheme, Host: parsed.Host}, nil
}

// printableASCII reports whether host holds only printable ASCII with no
// space. A Cyrillic look-alike or a non-breaking space would otherwise pass
// url.Parse and fail only on the first request.
func printableASCII(host string) bool {
	for i := 0; i < len(host); i++ {
		if host[i] <= ' ' || host[i] > '~' {
			return false
		}
	}
	return true
}

// validPort reports whether port is empty (the scheme default) or a decimal
// number in 1-65535.
func validPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}
