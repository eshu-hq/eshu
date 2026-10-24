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
	// Count the '@' in the whole value text, before comment truncation and
	// quote stripping, so a credential that net/url splits out of the userinfo
	// cannot hide in the part the parser discards.
	scheme, hostname, hostPort, urlPath, ok := sanitizeRegistryURL(raw, strings.Count(valuePart, "@"))
	if !ok {
		return nil
	}
	withheld := !registryPathAllowed(hostname, urlPath)
	if withheld {
		// A path off the documented layouts is dropped; the scheme and host
		// still answer which registry the repository uses.
		urlPath = ""
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
	if withheld {
		row["registry_path_withheld"] = true
	}
	return row
}

// registryValue extracts the value text the way the npm ini reader does. A
// quoted value is the text inside the quotes; a backslash inside quotes is not
// interpreted, so the line is skipped. An unquoted value ends at the first
// unescaped `;` or `#`, and a backslash before `;`, `#`, or `\` makes that
// character literal. It rejects a value with an environment expansion, a
// control character, or a non-ASCII byte.
func registryValue(valuePart string) (string, bool) {
	value := strings.Trim(valuePart, " \t")
	if value != "" && (value[0] == '"' || value[0] == '\'') {
		end := strings.IndexByte(value[1:], value[0])
		if end < 0 {
			return "", false
		}
		value = value[1 : 1+end]
		if strings.IndexByte(value, '\\') >= 0 {
			return "", false
		}
	} else {
		value = unescapeIniValue(value)
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

// unescapeIniValue follows the npm `ini` reader for an unquoted value: it stops
// at the first unescaped `;` or `#`, turns `\;`, `\#`, and `\\` into the bare
// character, and keeps any other backslash pair as written.
func unescapeIniValue(value string) string {
	var out strings.Builder
	escaped := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case escaped:
			if c != ';' && c != '#' && c != '\\' {
				out.WriteByte('\\')
			}
			out.WriteByte(c)
			escaped = false
		case c == ';' || c == '#':
			return strings.TrimRight(out.String(), " \t")
		case c == '\\':
			escaped = true
		default:
			out.WriteByte(c)
		}
	}
	if escaped {
		out.WriteByte('\\')
	}
	return strings.TrimRight(out.String(), " \t")
}

// sanitizeRegistryURL parses one registry value and returns its scheme, bare
// host, host with port, and path. It drops the userinfo, query, and fragment,
// and it rejects a value that is not an absolute http or https URL with a
// valid host. atCount is the number of '@' in the whole value text before any
// truncation. net/url ends the authority at the first '/', '?', or '#', and
// accepts an empty or all-digit port, so a value such as
// `https://user:/pw@host/` parses with the username as the host and the
// password in the path. The line is rejected unless it has no '@' at all, or
// exactly one that net/url consumed as the userinfo delimiter. It never returns
// a parse error, because url.Parse errors quote the input.
func sanitizeRegistryURL(raw string, atCount int) (scheme, hostname, hostPort, urlPath string, ok bool) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" {
		return "", "", "", "", false
	}
	scheme = strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", "", "", false
	}
	hostname = strings.ToLower(parsed.Hostname())
	if hostname == "" {
		return "", "", "", "", false
	}
	// One '@' with no userinfo is the Azure Artifacts feed view
	// (_packaging/my-feed@Release) or a split-userinfo attack. Only the first is
	// kept, and only on an Azure host.
	azureView := atCount == 1 && parsed.User == nil && azureFeedViewURL(parsed, hostname)
	if atCount > 1 || atCount == 1 && parsed.User == nil && !azureView {
		return "", "", "", "", false
	}
	host := hostname
	if strings.Contains(host, ":") {
		ip := net.ParseIP(host)
		if ip == nil || strings.Contains(host, "%") {
			return "", "", "", "", false
		}
		host = "[" + host + "]"
	} else if !hostPattern.MatchString(host) {
		return "", "", "", "", false
	}
	hostPort = host
	if port := parsed.Port(); port != "" {
		hostPort += ":" + port
	}
	urlPath = parsed.EscapedPath()
	if azureView {
		// Escape the view separator so no emitted field holds an '@'.
		urlPath = strings.Replace(urlPath, "@", "%40", 1)
	}
	if len(urlPath) > maxPathBytes {
		return "", "", "", "", false
	}
	return scheme, hostname, hostPort, urlPath, true
}

// azureFeedViewURL reports whether parsed is an Azure Artifacts URL whose only
// '@' sits in the path: an Azure host with no port, no query, no fragment, and
// no '@' in the authority (parsed.User is nil), and exactly one '@' in the path.
// The path still has to match an Azure layout with a name-shaped feed and view
// before it is kept (registryPathAllowed); otherwise the row is host-only.
func azureFeedViewURL(parsed *url.URL, hostname string) bool {
	return isAzureArtifactsHost(hostname) && !strings.Contains(parsed.Host, ":") &&
		parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" &&
		strings.Count(parsed.EscapedPath(), "@") == 1
}
