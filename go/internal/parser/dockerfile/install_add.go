// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dockerfile

import "strings"

// parseADDInstall records every direct HTTP(S) source after Dockerfile ADD
// options. Unknown option shapes remain unparsed rather than turning an
// option value into an artifact name.
func parseADDInstall(item instruction, stage string, variables *installScope) ([]Install, []UnparsedCommand, int) {
	words, ok := shellWords(item.value)
	if !ok || len(words) < 2 {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "invalid_add"}}, 0
	}
	if strings.HasPrefix(words[0], "[") {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "unsupported_add_form"}}, 0
	}
	index, dropped := 0, 0
	for index < len(words) && strings.HasPrefix(words[index], "--") {
		name, value, hasValue := strings.Cut(words[index], "=")
		if !validADDOption(name) || !hasValue || value == "" {
			return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "unsupported_add_option"}}, countCredentialValues(words, variables)
		}
		if sensitiveInstallValue("", value, variables) {
			dropped++
		}
		index++
	}
	if len(words)-index < 2 {
		return nil, []UnparsedCommand{{LineNumber: item.line, Reason: "invalid_add"}}, dropped
	}
	installs := make([]Install, 0)
	unparsed := make([]UnparsedCommand, 0)
	for _, source := range words[index : len(words)-1] {
		if !strings.Contains(source, "://") && !strings.Contains(source, "$") {
			continue
		}
		expanded, resolved, sensitive := expandInstallWord(source, variables)
		if sensitive {
			dropped++
			continue
		}
		if !resolved {
			unparsed = append(unparsed, UnparsedCommand{LineNumber: item.line, Reason: "unresolved_variable"})
			continue
		}
		if !strings.Contains(expanded, "://") {
			continue
		}
		name, credential, valid := safeDownload(expanded)
		if credential {
			dropped++
			continue
		}
		if !valid {
			unparsed = append(unparsed, UnparsedCommand{LineNumber: item.line, Reason: "invalid_url"})
			continue
		}
		installs = append(installs, Install{Manager: "url", Name: name, LineNumber: item.line, Stage: stage})
	}
	return installs, unparsed, dropped
}

func validADDOption(name string) bool {
	switch name {
	case "--keep-git-dir", "--checksum", "--chmod", "--chown", "--link", "--unpack", "--exclude":
		return true
	default:
		return false
	}
}
