// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package npmrc

import (
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

const (
	// LanguageName is the parser language key and the lang value on each row.
	LanguageName = "npmrc"
	// PackageManager is the package_manager value on each row.
	PackageManager = "npm"
	// ConfigKind is the config_kind of a registry-URL row.
	ConfigKind = "registry_url"

	// maxLineBytes bounds one line. A longer line cannot be a registry URL an
	// operator wrote, and bounding it keeps the URL parse and the detector
	// scan cheap on hostile input.
	maxLineBytes = 8 << 10
	// maxPathBytes bounds the emitted URL path.
	maxPathBytes = 512
)

var (
	// scopeKeyPattern matches the `@scope:registry` key. The scope is the npm
	// package scope, a public namespace, never a credential.
	scopeKeyPattern = regexp.MustCompile(`^@[A-Za-z0-9][A-Za-z0-9._~-]*:registry$`)
	// hostPattern matches a DNS-style host. IPv6 literals are checked with
	// net.ParseIP.
	hostPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)
)

// Parse reads one `.npmrc` file and returns the parser payload. The only rows
// it emits are registry-URL rows with the userinfo, query, and fragment
// removed. A file with no accepted registry line returns an empty payload.
// The error text never quotes file content.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	_ = options // IndexSource is ignored on purpose: the body is never echoed.
	payload := shared.BasePayload(path, LanguageName, isDependency)

	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}

	rows := make([]map[string]any, 0, 4)
	inSection := false
	lineNumber := 0
	text := strings.TrimPrefix(string(source), "\xef\xbb\xbf")
	for len(text) > 0 {
		var line string
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			line, text = text[:i], text[i+1:]
		} else {
			line, text = text, ""
		}
		lineNumber++
		line = strings.Trim(line, " \t\r")
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if line[0] == '[' && line[len(line)-1] == ']' {
			// Every key after a section header lives under that section, and
			// registry keys are top-level.
			inSection = true
			continue
		}
		if inSection || len(line) > maxLineBytes {
			continue
		}
		if row := registryRow(line, lineNumber); row != nil {
			rows = append(rows, row)
		}
	}
	payload["variables"] = rows
	return payload, nil
}

// registryRow builds the row for one non-comment line, or nil when the line is
// not a registry line or its value fails sanitization.
func registryRow(line string, lineNumber int) map[string]any {
	keyPart, valuePart, found := strings.Cut(line, "=")
	if !found {
		return nil
	}
	key := strings.Trim(keyPart, " \t")
	section, scope := "", ""
	switch {
	case key == "registry":
		section = "registry"
	case scopeKeyPattern.MatchString(key):
		section = "scope_registry"
		scope = key[:strings.IndexByte(key, ':')]
	default:
		return nil
	}
	raw, ok := registryValue(valuePart)
	if !ok {
		return nil
	}
	scheme, hostPort, urlPath, ok := sanitizeRegistryURL(raw)
	if !ok {
		return nil
	}
	value := scheme + "://" + hostPort + urlPath
	if len(detect.Spans(value)) > 0 {
		// The shared detector is the same one the credential-file policy
		// trusts, so a registry URL it flags is dropped, not emitted.
		return nil
	}
	row := map[string]any{
		"name":            key,
		"line_number":     lineNumber,
		"value":           value,
		"section":         section,
		"config_kind":     ConfigKind,
		"package_manager": PackageManager,
		"lang":            LanguageName,
		"lockfile":        false,
		"registry_scheme": scheme,
		"registry_host":   hostPort,
		"registry_path":   urlPath,
	}
	if scope != "" {
		row["registry_scope"] = scope
	}
	return row
}

// registryValue extracts the value text the way the npm ini reader does: a
// quoted value is the text inside the quotes, and an unquoted value ends at
// the first `;` or `#`. It rejects a value with an environment expansion, a
// control character, or a non-ASCII byte.
func registryValue(valuePart string) (string, bool) {
	value := strings.Trim(valuePart, " \t")
	if value != "" && (value[0] == '"' || value[0] == '\'') {
		end := strings.IndexByte(value[1:], value[0])
		if end < 0 {
			return "", false
		}
		value = value[1 : 1+end]
	} else if i := strings.IndexAny(value, ";#"); i >= 0 {
		value = strings.TrimRight(value[:i], " \t")
	}
	if value == "" || strings.Contains(value, "${") {
		return "", false
	}
	for i := 0; i < len(value); i++ {
		if c := value[i]; c < 0x20 || c >= 0x7f {
			return "", false
		}
	}
	return value, true
}

// sanitizeRegistryURL parses one registry value and returns its scheme, host
// with port, and path. It drops the userinfo, query, and fragment, and it
// rejects a value that is not an absolute http or https URL with a valid host.
// It never returns a parse error, because url.Parse errors quote the input.
func sanitizeRegistryURL(raw string) (scheme, hostPort, urlPath string, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" {
		return "", "", "", false
	}
	scheme = strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", "", false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return "", "", "", false
	}
	if strings.Contains(host, ":") {
		ip := net.ParseIP(host)
		if ip == nil || strings.Contains(host, "%") {
			return "", "", "", false
		}
		host = "[" + host + "]"
	} else if !hostPattern.MatchString(host) {
		return "", "", "", false
	}
	hostPort = host
	if port := parsed.Port(); port != "" {
		hostPort += ":" + port
	}
	urlPath = parsed.EscapedPath()
	if len(urlPath) > maxPathBytes {
		return "", "", "", false
	}
	return scheme, hostPort, urlPath, true
}
