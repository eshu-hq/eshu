// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"fmt"
	"regexp"
	"strings"
)

// Verify is the output-membership belt. It scans the canonical cassette bytes
// with the same alternatives the private-data gate
// (scripts/lib/cassette_private_data_pattern.sh) applies -- ipv4, nodeip,
// ipv6, account12, arn, hostname -- and refuses any candidate that is neither
// a documented safe form nor a pseudonym this run produced. The one place
// produced matters is the reserved 0000xxxxxxxx account form: the gate admits
// it by shape, Verify admits it only when this run minted it, which closes
// the residual of a raw account that itself starts with 0000.
//
// The identifier alternative (organisation literals from
// ESHU_PRIVATE_IDENTIFIERS_FILE) is the gate's alone; Verify has no list.
//
// The error names offsets and alternatives only, never a value.
func Verify(canonical []byte, produced Set) error {
	text := string(canonical)
	var refused []string
	add := func(offset int, alternative string) {
		refused = append(refused, fmt.Sprintf("offset %d alternative %s", offset, alternative))
	}
	scanAccounts(text, produced, add)
	scanIPv4(text, add)
	scanNodeIP(text, add)
	scanIPv6(text, add)
	scanARNs(text, produced, add)
	scanHostnames(text, produced, add)
	if len(refused) == 0 {
		return nil
	}
	shown := refused
	if len(shown) > 20 {
		shown = shown[:20]
	}
	return fmt.Errorf("recordpseudo: verify refused %d candidate(s) that are neither a documented safe form nor a pseudonym this run produced: %s", len(refused), strings.Join(shown, "; "))
}

type refusal func(offset int, alternative string)

var (
	digitRunRe = regexp.MustCompile(`[0-9]+`)
	ipv4Cand   = regexp.MustCompile(`[0-9]{1,3}(?:\.[0-9]{1,3}){3}`)
	nodeIPRe   = regexp.MustCompile(`(?i)ip-(?:[0-9]{1,3}-){3}[0-9]{1,3}`)
	hexRunRe   = regexp.MustCompile(`[0-9A-Fa-f:]+`)
	ipv6Shape  = regexp.MustCompile(`^(?:(?:[0-9A-Fa-f]{1,4}:){3,7}[0-9A-Fa-f]{1,4}|(?:[0-9A-Fa-f]{1,4}:){1,7}:(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){0,6})?|::(?:[0-9A-Fa-f]{1,4}(?::[0-9A-Fa-f]{1,4}){0,6}))$`)
	arnCand    = regexp.MustCompile(`arn:aws(?:-[a-z]+)*:[a-z0-9-]*:[a-z0-9-]*:[^:"\s]*:`)
)

// scanAccounts: twelve digits bounded by non-hex on both sides (the Ifá form
// matched inside sha256 digests because hex letters read as boundaries).
func scanAccounts(text string, produced Set, refuse refusal) {
	for _, loc := range digitRunRe.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if end-start != 12 {
			continue
		}
		if (start > 0 && isHex(text[start-1])) || (end < len(text) && isHex(text[end])) {
			continue
		}
		if !accountAllowed(text[start:end], produced) {
			refuse(start, "account12")
		}
	}
}

// scanIPv4: a dotted quad not preceded by a digit or "digit." and not
// followed by a digit or ".digit", each octet 0-255.
func scanIPv4(text string, refuse refusal) {
	for _, loc := range ipv4Cand.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && (isDigit(text[start-1]) || (text[start-1] == '.' && start > 1 && isDigit(text[start-2]))) {
			continue
		}
		if end < len(text) && (isDigit(text[end]) || (text[end] == '.' && end+1 < len(text) && isDigit(text[end+1]))) {
			continue
		}
		token := text[start:end]
		if !validOctets(token) {
			continue
		}
		if !ipv4Allowed(token) {
			refuse(start, "ipv4")
		}
	}
}

func validOctets(quad string) bool {
	for _, octet := range strings.Split(quad, ".") {
		if len(octet) > 1 && octet[0] == '0' {
			// "01" is not a decimal octet the gate accepts ([1-9]?[0-9]).
			return false
		}
		n := 0
		for _, c := range octet {
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}

func scanNodeIP(text string, refuse refusal) {
	for _, loc := range nodeIPRe.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && (isAlnum(text[start-1]) || text[start-1] == '-') {
			continue
		}
		if end < len(text) && isDigit(text[end]) {
			continue
		}
		if !nodeIPAllowed(strings.ToLower(text[start:end])) {
			refuse(start, "nodeip")
		}
	}
}

// scanIPv6: maximal hex/colon runs whose neighbours are not letters, digits,
// colons, dots or hyphens (left) or letters, digits, colons (right), matched
// against the gate's three shapes. Six-group MAC addresses land here too.
func scanIPv6(text string, refuse refusal) {
	for _, loc := range hexRunRe.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if start > 0 && (isAlnum(text[start-1]) || strings.IndexByte(":.-", text[start-1]) >= 0) {
			continue
		}
		if end < len(text) && (isAlnum(text[end]) || text[end] == ':') {
			continue
		}
		token := text[start:end]
		if !ipv6Shape.MatchString(token) {
			continue
		}
		if !ipv6Allowed(strings.ToLower(token)) {
			refuse(start, "ipv6")
		}
	}
}

func scanARNs(text string, produced Set, refuse refusal) {
	for _, loc := range arnCand.FindAllStringIndex(text, -1) {
		parts := strings.SplitN(text[loc[0]:loc[1]], ":", 6)
		account := parts[4]
		if account == "" || account == "aws" || accountAllowed(account, produced) {
			continue
		}
		refuse(loc[0], "arn")
	}
}

// scanHostnames: a dotted name ending in a listed TLD, not preceded by a
// label character or by "label." and not followed by a label character or
// underscore (that right boundary is part of hostnameCand); the allow forms
// are the gate's, plus ECR under a produced account.
func scanHostnames(text string, produced Set, refuse refusal) {
	for _, loc := range hostnameCand.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if end > start && !isAlnum(text[end-1]) {
			end-- // the boundary byte the pattern consumed
		}
		if start > 0 {
			prev := text[start-1]
			if isAlnum(prev) || prev == '-' {
				continue
			}
			if prev == '.' && start > 1 && isAlnum(text[start-2]) {
				continue
			}
		}
		if !hostnameAllowed(strings.ToLower(text[start:end]), produced) {
			refuse(start, "hostname")
		}
	}
}
