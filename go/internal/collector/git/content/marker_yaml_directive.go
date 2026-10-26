// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import "strings"

// hasYAMLVersionDirective reports whether any source line is a "%"-directive
// line as js-yaml 4.1.1 reads it: each "\n"-terminated source line is first
// split on the remaining breaks both downstream parsers honor (a carriage
// return, NEL U+0085, LS U+2028, PS U+2029), then a leading byte-order mark
// and leading horizontal white space are skipped, and "%" must be followed by
// a byte that is neither space nor tab. js-yaml takes any such run as the
// directive name, warning on unknown names ("%FOO", "%YAM", "%ABC", "%x",
// "%YAMLx", "%YAMLé", "%YAMLL", "%%YAML") yet reading on, while yaml.v3
// rejects each (unknown directive name / expected directive name /
// non-alphabetical character), so the parsed walk is blind and only the
// lexical fallback covers them. A bare "%" and "% YAML" are not matched:
// js-yaml rejects both for an empty name, and yaml.v3 does too. Only a
// break-separated segment starting with "%" counts, so URL-encoded values
// holding "%" mid-line ("abc%YAML 1.2", "x%20y") are not directives. A
// tab-indented directive is matched because js-yaml 4.1.1 reads it (yaml.v3
// and PyYAML 6.0.3 reject it); a space-indented one and a bare "%YAML" are
// matched conservatively though every probed parser rejects them there. A
// directive split from its name across NEL, LS, or PS ("%"+break+"YAML 1.2"
// on one "\n"-line) is matched, as are its neighbors: a space or tab after
// the break ("%"+break+" YAML 1.2"), a trailing break with no name on the
// line ("%"+break), and a multi-break run ("%"+NEL+NEL+"YAML 1.2").
// js-yaml's directive scanner stops a name only at space, tab, LF, or CR, so
// the break byte itself joins the name and each neighbor reads on, while
// yaml.v3 rejects each (could not find expected directive name) and PyYAML
// 6.0.3 rejects each while scanning a directive. A "%" split from its name
// across "\r" or "\n" stays unmatched: every probed parser rejects that
// shape. A break-prefixed directive ("\r", NEL, LS, or PS before "%", or a
// directive on a later break-separated segment) is matched because a
// downstream parser still decodes the token while yaml.v3 rejects the body
// (found incompatible YAML document), so the parsed walk covers nothing and
// only the lexical fallback covers it: a CR-prefixed directive reads as a
// directive line in both downstream parsers, while a leading NEL, LS, or PS
// leaves js-yaml 4.1.1 with a scalar first document (directive not consumed)
// that still decodes the token, and PyYAML 6.0.3 reads each as a directive.
// A late break-separated directive reads in PyYAML while js-yaml rejects it.
// A VT- or FF-indented "%" has no reader in the probed parsers and stays
// unmatched. An NBSP-indented "%" stays unmatched for a different reason:
// both downstream parsers still decode the token in the "---" envelope, but
// yaml.v3 parses that body (two documents, the same AST as the covered
// flat-quoted shape), so the parsed walk already covers it. A BOM-prefixed
// directive is matched: both downstream parsers read it while yaml.v3
// rejects it.
func hasYAMLVersionDirective(lines []string) bool {
	for _, line := range lines {
		segments, breaks := splitYAMLBreaks(line)
		for i, segment := range segments {
			rest := strings.TrimLeft(segment, " \t\uFEFF")
			if len(rest) >= 2 && rest[0] == '%' && rest[1] != ' ' && rest[1] != '\t' {
				return true
			}
			// A lone "%" followed by NEL, LS, or PS on the same "\n"-line is the
			// split-directive hole above with its neighbors: the break byte
			// itself joins js-yaml's directive name, so what follows on the
			// line is irrelevant — empty, space- or tab-leading, or further
			// breaks. Only a "\r" first separator stays unmatched:
			// "%"+"\r"+name is rejected by every probed parser, as is
			// "%"+"\n"+name.
			if rest == "%" && i+1 < len(segments) && breaks[i] != '\r' {
				return true
			}
		}
	}
	return false
}

// splitYAMLBreaks splits a "\n"-terminated source line on the line breaks the
// sole caller's split misses yet js-yaml 4.1.1 and PyYAML 6.0.3 honor as line
// breaks before a directive: a carriage return, NEL U+0085, LS U+2028, and PS
// U+2029. It also returns the break rune that ends each segment except the
// last, so the caller can tell a "%"-to-name join across NEL, LS, or PS apart
// from one across "\r". Segments hold none of those bytes, so the "%"-name
// check in hasYAMLVersionDirective needs no per-break exclusion after the
// split.
func splitYAMLBreaks(line string) ([]string, []rune) {
	var segments []string
	var breaks []rune
	start := 0
	for i, r := range line {
		if r == '\r' || r == '\u0085' || r == '\u2028' || r == '\u2029' {
			segments = append(segments, line[start:i])
			breaks = append(breaks, r)
			start = i + len(string(r))
		}
	}
	return append(segments, line[start:]), breaks
}
