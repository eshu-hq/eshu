// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// skeletonSeeds is the credential-shape corpus the #8833 property test plants.
// Each entry is the planted text and the token searched for in the output.
var skeletonSeeds = []struct{ planted, token string }{
	{"AKIAZQSEEDX012345678", "ZQSEEDX"},
	{"sk_live_ZqSeedX12345678", "ZqSeedX1"},
	{"xoxb-ZqSeedX-0123456789", "ZqSeedX-0"},
	{"MIIEZqSeedXbase64bodyAAAA==", "MIIEZqSeedX"},
	{"ZqSeedXdGhpcyBpcyBhIHNlY3JldA==", "ZqSeedXdGhp"},
	{"0a1b2c3d4e5f60718293a4b5c6d7e8f9", "0a1b2c3d4e5f6071"},
	{"Zq!Seed#X$2026^pw", "Zq!Seed#X"},
	{"https://svc:ZqSeedXpw@db.internal/x", "ZqSeedXpw"},
	{"hunter2ZqSeedX", "hunter2ZqSeedX"},
	{"https://svc:GhSeedXpw@github.com/org/repo?token=GhQuerySeed", "GhSeedXpw"},
	{"https://github.com/org/repo?access_token=GhQuerySeedTwo#frag", "GhQuerySeedTwo"},
	{"see https://svc:GhSeedThreePw@github.com/org/repo#GhFragSeed now", "GhSeedThreePw"},
}

var skeletonKeys = []string{"region", "db_password", "api_token", "client_secret", "stripe", "tags", "Name", "value", "conn"}

// skeletonLine renders one random raw line shape around a seed. Multi-line
// shapes (heredoc, PEM, block comment, nested block) return several lines.
func skeletonLine(rng *rand.Rand, seed string) []string {
	key := skeletonKeys[rng.IntN(len(skeletonKeys))]
	indent := strings.Repeat(" ", rng.IntN(3)*2)
	switch rng.IntN(32) {
	case 0:
		return []string{indent + key + " = \"" + seed + "\""}
	case 1:
		return []string{indent + "\"" + key + "\": \"" + seed + "\","}
	case 2:
		return []string{indent + key + " = " + seed}
	case 3:
		return []string{indent + "# rotate " + seed + " soon"}
	case 4:
		return []string{indent + "// " + seed}
	case 5:
		return []string{indent + "\"" + seed + "\","}
	case 6:
		return []string{key + " = <<EOT", "  " + seed, "  export X=" + seed, "EOT"}
	case 7:
		return []string{"# -----BEGIN RSA PRIVATE KEY-----", "# " + seed, "# -----END RSA PRIVATE KEY-----"}
	case 8:
		return []string{key + " = <<-PEM", "-----BEGIN PRIVATE KEY-----", seed, "-----END PRIVATE KEY-----", "PEM"}
	case 9:
		return []string{indent + key + " = \"prefix-" + seed + "-suffix\""}
	case 10:
		return []string{indent + key + " = \"" + seed}
	case 11:
		return []string{"/* " + seed, " " + seed + " = true", "*/"}
	case 12:
		return []string{indent + key + " = 3 # " + seed}
	case 13:
		return []string{indent + key + " = {", indent + "  inner = \"" + seed + "\"", indent + "}"}
	case 14:
		return []string{indent + key + " = [", indent + "  \"" + seed + "\",", indent + "]"}
	case 15:
		return []string{indent + key + " = [\"" + seed + "\", \"x\"]"}
	case 16:
		return []string{indent + "db_password = \"" + seed + "\""}
	case 17:
		return []string{"k = \"-----BEGIN PRIVATE KEY-----\\n" + seed + "\\n-----END PRIVATE KEY-----\""}
	case 30:
		// app_repo and app_name keep a plain-name value by design (the 8813
		// record's evidence shapes), so only URL seeds are planted under them;
		// TestRedactSkeletonKeepsEvidenceShapes pins the name case.
		if !strings.Contains(seed, "://") {
			return []string{indent + key + " = \"" + seed + "\""}
		}
		return []string{indent + "app_repo = \"" + seed + "\"", indent + "\"" + seed + "\","}
	case 18:
		// Noise the allowlist keeps.
		return []string{indent + key + " = " + []string{"true", "3", "-1.5", "var.x", "local.y", "module.m.id", "\"1.2.3\"", "\"~> 5.0\"", "null"}[rng.IntN(9)]}
	case 19:
		// A token wrapped as a reference (rev-8833 F1). A plain name the
		// detector does not flag is a reference name and kept by design, so
		// only a detector-flagged seed is wrapped.
		if len(detect.Spans(seed)) == 0 {
			return []string{indent + key + " = \"" + seed + "\""}
		}
		return []string{indent + key + " = " + []string{"var.", "module.", "data."}[rng.IntN(3)] + seed + []string{"", ".id"}[rng.IntN(2)]}
	case 20:
		// A bare token with base64 padding (rev-8833 F2).
		return []string{indent + seed + "==" + []string{"", ","}[rng.IntN(2)]}
	case 21:
		// A heredoc opened inside a list (rev-8833 F3).
		return []string{indent + key + " = [<<EOF", seed + ": true", seed + "==", "EOF", indent + "]"}
	case 22:
		// A block comment opened after a value (rev-8833 F3).
		return []string{indent + key + " = 1 /* old", seed + ": true", "*/"}
	case 23:
		return []string{indent + key + " = jsonencode(<<-EOT", "  " + seed + ": true", "  EOT", indent + ")"}
	case 24:
		return []string{indent + "# rotated", indent + seed + "=="}
	case 25:
		// Comment state from the whole line (rev-8833 G1).
		return []string{indent + key + " = 1", "/* c */ " + key + " = 2 /* d", seed + ": 1", "*/"}
	case 26:
		return []string{"/* a", "*/ " + key + " = 1 /* b", seed + ": 1", "*/"}
	case 27:
		return []string{indent + key + " = 1 /* see <<EOF", "EOF", seed + ": 1", "*/"}
	case 28:
		// A heredoc opens after a one-line comment closes on the same line.
		return []string{"/* c */ " + key + " = <<EOF", seed + ": 1", "EOF"}
	case 29:
		return []string{"/* a", "*/ " + key + " = <<EOF", seed + ": 1", "EOF"}
	default:
		return []string{""}
	}
}

func TestRedactSkeletonPropertyCorpus(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(8833, 8813))
	for i := 0; i < 3000; i++ {
		var raw []string
		var planted []string
		for n := 1 + rng.IntN(12); n > 0; n-- {
			seed := skeletonSeeds[rng.IntN(len(skeletonSeeds))]
			raw = append(raw, skeletonLine(rng, seed.planted)...)
			planted = append(planted, seed.token)
		}
		body := strings.Join(raw, "\n") + "\n"
		checkSkeletonProperties(t, body)
		out, _, _ := redactSkeleton(body)
		for _, token := range planted {
			if strings.Contains(out, token) {
				t.Fatalf("planted %q survived:\n in\n%s\n out\n%s", token, body, out)
			}
		}
	}
}

// checkSkeletonProperties asserts the invariants every skeleton body holds.
func checkSkeletonProperties(t *testing.T, body string) {
	t.Helper()
	out, _, err := redactSkeleton(body)
	if err != nil {
		t.Fatalf("redactSkeleton(%q): %v", body, err)
	}
	rawLines, outLines := strings.Split(body, "\n"), strings.Split(out, "\n")
	if len(rawLines) != len(outLines) {
		t.Fatalf("line count %d -> %d:\n%q\n%q", len(rawLines), len(outLines), body, out)
	}
	if again, _, _ := redactSkeleton(out); again != out {
		t.Fatalf("not a fixed point:\n in   %q\n out  %q\n out2 %q", body, out, again)
	}
	if err := skeletonGrammar(outLines); err != nil {
		t.Fatalf("%v\n in  %q\n out %q", err, body, out)
	}
	for i := range rawLines {
		raw, stored := strings.TrimSuffix(rawLines[i], "\r"), strings.TrimSuffix(outLines[i], "\r")
		// The (line_number, finding_kind) differential, per line: the detector
		// migration stores the same finding for the skeleton line as for the
		// raw line, for all six kinds and for no finding.
		if want, got := rawFindingKind(raw), rawFindingKind(stored); got != want {
			t.Fatalf("line %d: raw finding %q, stored finding %q: %q -> %q", i+1, want, got, raw, stored)
		}
		if rawFindingKind(raw) == "" && holdsKindMarker(stored) && stored != raw {
			t.Fatalf("line %d: no raw finding but a kind marker was added: %q -> %q", i+1, raw, stored)
		}
	}
}

// rawFindingKind is the detector migration on one line (detect.Finding).
func rawFindingKind(line string) string {
	return detect.Finding(line)
}

func holdsKindMarker(line string) bool {
	for _, kind := range detect.Kinds() {
		if strings.Contains(line, markerPrefix+kind) {
			return true
		}
	}
	return false
}

// skeletonGrammar is the test's own statement of the stored-line grammar: a
// structural line, key = <allowlist value>, key = REDACTED_<kind>,
// REDACTED_<kind> or REDACTED_value alone, a heredoc opener and its own
// closer, or an empty line. It shares no expression with the redactor.
func skeletonGrammar(lines []string) error {
	heredoc := ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if heredoc != "" && trimmed == heredoc {
			heredoc = ""
			continue
		}
		if grammarURLComment(trimmed) {
			continue
		}
		if trimmed == "" || isGrammarMarker(trimmed) || grammarEvidence(strings.TrimSuffix(trimmed, ",")) ||
			slices.Contains([]string{"{", "}", "[", "]", "},", "],", "{}", "[]", "{},", "[],"}, trimmed) {
			continue
		}
		value, ok := grammarValue(trimmed)
		if !ok {
			return fmt.Errorf("line %d %q is outside the skeleton grammar", i+1, line)
		}
		value = strings.TrimSpace(strings.TrimSuffix(value, ","))
		if left, right, ok := strings.Cut(value, " # "); ok && grammarURLComment("# "+right) {
			value = left
		}
		switch {
		case value == "{" || value == "[" || value == "{}" || value == "[]":
		case strings.HasPrefix(value, "<<"):
			heredoc = strings.TrimPrefix(strings.TrimPrefix(value, "<<"), "-")
		case isGrammarMarker(value) || grammarAllowed(value) || grammarEvidence(value):
		default:
			return fmt.Errorf("line %d value %q is neither a marker nor an allowlist shape", i+1, value)
		}
	}
	return nil
}

func isGrammarMarker(text string) bool {
	if text == markerPrefix+"value" {
		return true
	}
	return slices.Contains(detect.Kinds(), strings.TrimPrefix(text, markerPrefix)) && strings.HasPrefix(text, markerPrefix)
}

// grammarValue splits key = value or "key": value by hand and returns the
// value.
func grammarValue(line string) (string, bool) {
	var rest string
	if strings.HasPrefix(line, `"`) {
		end := strings.IndexByte(line[1:], '"')
		if end < 0 {
			return "", false
		}
		rest = line[end+2:]
	} else {
		end := strings.IndexAny(line, " \t=:")
		if end <= 0 || strings.IndexFunc(line[:end], isNotKeyRune) >= 0 {
			return "", false
		}
		rest = line[end:]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" || (rest[0] != '=' && rest[0] != ':') {
		return "", false
	}
	return strings.TrimSpace(rest[1:]), true
}

func isNotKeyRune(r rune) bool {
	return r != '_' && r != '-' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9')
}

// grammarAllowed restates the allowlist by hand: literals, decimal numbers,
// var./local./module./data. references, and quoted version constraints.
func grammarAllowed(value string) bool {
	switch value {
	case "true", "false", "null":
		return true
	}
	if strings.Trim(value, "0123456789.eE+-") == "" {
		// A literal past float64 (1E1000) is still a number: ErrRange is not a
		// syntax error.
		_, err := strconv.ParseFloat(value, 64)
		return err == nil || errors.Is(err, strconv.ErrRange)
	}
	if root, rest, ok := strings.Cut(value, "."); ok && slices.Contains([]string{"var", "local", "module", "data"}, root) {
		return strings.Trim(rest, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.[]") == ""
	}
	if len(value) < 3 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	for _, part := range strings.Split(value[1:len(value)-1], ",") {
		part = strings.TrimLeft(strings.TrimSpace(part), "~><=! ")
		part = strings.TrimPrefix(part, "v")
		core, pre, _ := strings.Cut(part, "-")
		if pre != "" && !slices.Contains([]string{"alpha", "beta", "rc", "pre", "dev", "snapshot"}, strings.TrimRight(pre, "0123456789.")) {
			return false
		}
		if strings.Count(core, ".") < 1 || strings.Trim(core, "0123456789.") != "" {
			return false
		}
	}
	return true
}

// FuzzRedactSkeleton checks the same invariants on arbitrary input, plus a
// planted credential after the input that never survives. Under go test it
// runs the seed corpus; run it with -fuzz for a longer search.
func FuzzRedactSkeleton(f *testing.F) {
	for _, s := range []string{
		"a = 1\n", "x = <<EOT\nb\nEOT\n", "# -----BEGIN PRIVATE KEY-----\nabc\n", "\"k\": \"v\",\n",
		"/* c\n d\n*/\n", "k = \"unterminated\n", "{\n  \"a\": [\n    1,\n  ]\n}\n", "password　= abcdefghij\n",
		"ſecret = abcdefghij\n", "k = REDACTED_value\n", "  REDACTED_api_token\n", "k = \"~> 1.2\"\n",
		// The rev-8833 F1-F3 shapes.
		"x = module.sk_" + "live_4eC39HqLyjWDarjtT1zdp7dc\n", "dGhpc2lzYXNlY3JldHRva2Vu==\n", "X==,\n",
		"keys = [<<EOF\nPassw0rdHunter2: true\nEOF\n]\n", "x = 1 /* old\nPassw0rdHunter2: true\n*/\n",
		"/* c */ y = 2 /* d\nPassw0rdHunter2: 1\n*/\n", "/* a\n*/ x = 1 /* b\nPassw0rdHunter2: 1\n*/\n",
		"x = 1 /* see <<EOF\nEOF\nPassw0rdHunter2: 1\n*/\n",
		"/* c */ x = <<EOF\nPassw0rdHunter2: 1\nEOF\n", "/* a\n*/ x = <<EOF\nPassw0rdHunter2: 1\nEOF\n",
		// The rev-8833 H1 family: <<ID in a line comment or a string is text.
		"x = 1 # <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\n", "x = 1 // <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\n",
		"x = 1 # <<-EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\n", "x = 1 # <<~EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\n",
		"x = \"a\" # <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\n", "x = \"a # <<EOF\n/* c\nEOF\nPassw0rdHunter2: 1\n*/\n",
		"x = 1 # <<EOF\n/* c */ y = 2 /* d\nEOF\nPassw0rdHunter2: 1\n*/\n",
		"x = \"${<<EOF\nPassw0rdHunter2: 1\nEOF\n}\"\n", "x = \"${\n}\" /* c\nPassw0rdHunter2: 1\n*/\n", "x = \"${\nPassw0rdHunter2: 1\n}\"\n",
		// The rev-8833 round-4 shapes: a heredoc stack, a sticky failure, and
		// the escaped openers.
		"x = <<EOF\n${ <<INNER\nEOF\nPassw0rdHunter2: 1\nINNER\n}\nEOF\nn = 2\n", "x = <<EOF\n${a}EOF\nPassw0rdHunter2: 1\nEOF\n",
		"x = \"abc\nPassw0rdHunter2: 1\n*/\n", "x = <<EOF\n$${ a\n%%{ b\nEOF\nPassw0rdHunter2: 1\n",
		"x = \"${ # c\n}\" /* d\nPassw0rdHunter2: 1\n*/\n", "x = <<EOF\r\nPassw0rdHunter2: 1\r\nEOF\r\n",
		strings.Repeat(`"${`, 17) + "\nPassw0rdHunter2: 1\n",
		// The rev-8833 round-5 shapes (J1): a bare \r in a string or heredoc text.
		"A=<<EOF\n\rEOF\n", "A=<<EOF\n\rEOF\nPassw0rdHunter2: 1\nEOF\nn = 2\n", "x = \"a\rb\"\nPassw0rdHunter2: 1\n",
		"x = \"a\\\rb\"\nPassw0rdHunter2: 1\n", "x = <<EOF\na\rb\nEOF\nPassw0rdHunter2: 1\n",
		"x = \"${ <<IN\n\r\nIN\n}\"\nPassw0rdHunter2: 1\n", "A = <<EOF\n$x \r $$y\nEOF\nPassw0rdHunter2: 1\n", "# a\rb\nn = 2\n", "n = 2\r\r\nm = 3\n",
		"x = \"${\"#\"}\" /* c\nPassw0rdHunter2: 1\n*/\n", "x = \"${\"//\"}\" <<EOF\nPassw0rdHunter2: 1\nEOF\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) || strings.Contains(in, "\x00") || len(in) > MaxGovernedBodyBytes {
			if _, _, err := redactSkeleton(in); err == nil {
				t.Fatalf("non-text input accepted: %q", in)
			}
			return
		}
		checkSkeletonProperties(t, in)
		var counts oracleCounts
		checkSkeletonOracle(t, in, &counts)
		for _, planted := range []string{
			in + "\npw = \"QqZzSeed9988\"\n",
			in + "\n# QqZzSeed9988\n",
			in + "\n\"QqZzSeed9988\",\n",
			in + "\nx = <<EOT\nQqZzSeed9988\nEOT\n",
			in + "\nx = var.sk_live_QqZzSeed99887766\n",
			in + "\nQqZzSeed9988==\n",
			in + "\nk = [<<EOF\nQqZzSeed9988: true\nEOF\n]\n",
			in + "\nk = 1 /* c\nQqZzSeed9988: true\n*/\n",
			in + "\n/* c */ y = 2 /* d\nQqZzSeed9988: 1\n*/\n",
			// The delimiter is private: an EOF line here could close a heredoc the
			// fuzzed input left open, and the seed would then sit in plain code.
			in + "\nx = 1 /* see <<QqZzEnd\nQqZzEnd\nQqZzSeed9988: 1\n*/\n",
			in + "\n/* c */ x = <<EOF\nQqZzSeed9988: 1\nEOF\n",
			in + "\n/* a\n*/ k = 1 /* b\nQqZzSeed9988: 1\n*/\n",
			// A <<ID in a line comment or a string opens no heredoc (rev-8833 H1).
			in + "\nk = 1 # <<QqZzEnd\n/* c\nQqZzEnd\nQqZzSeed9988: 1\n*/\n",
			in + "\nk = 1 // <<-QqZzEnd\n/* c\nQqZzEnd\nQqZzSeed9988: 1\n*/\n",
			in + "\nk = \"a # <<QqZzEnd\n/* c\nQqZzEnd\nQqZzSeed9988: 1\n*/\n",
			in + "\nk = \"${\"#\"}\" /* c\nQqZzSeed9988: 1\n*/\n",
			// An interpolation left open at the end of a line carries over.
			in + "\nk = \"${<<QqZzEnd\nQqZzSeed9988: 1\nQqZzEnd\n}\"\n",
			in + "\nk = \"${\n}\" /* c\nQqZzSeed9988: 1\n*/\n",
			in + "\nk = \"${\nQqZzSeed9988: 1\n}\"\n",
			in + "\nk = <<QqZzEnd\n${ <<QqZzIn\nQqZzEnd\nQqZzSeed9988: 1\nQqZzIn\n}\nQqZzEnd\n",
			in + "\nk = <<QqZzEnd\n${a}QqZzEnd\nQqZzSeed9988: 1\nQqZzEnd\n",
		} {
			out, _, err := redactSkeleton(planted)
			if err == nil && strings.Contains(out, "QqZzSeed9988") {
				t.Fatalf("planted seed survived: in=%q out=%q", planted, out)
			}
		}
	})
}

// grammarEvidence restates the kept evidence shapes by hand: a normalized
// github.com URL whose path is owner/repo of name characters, an OIDC subject,
// or (under app_repo and app_name, which this check cannot see, so any key) a
// plain owner/name. A planted credential in a URL's userinfo or query never
// fits it.
func grammarEvidence(value string) bool {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return false
	}
	inner := value[1 : len(value)-1]
	nameChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._-"
	if path, ok := strings.CutPrefix(inner, "https://github.com/"); ok {
		return strings.Count(path, "/") >= 1 && strings.Trim(path, nameChars+"/") == ""
	}
	if rest, ok := strings.CutPrefix(inner, "repo:"); ok {
		return strings.Trim(rest, nameChars+"/:*") == ""
	}
	return strings.Count(inner, "/") <= 1 && inner != "" && strings.Trim(inner, nameChars+"/") == ""
}

// grammarURLComment accepts a comment that holds only normalized github.com
// URLs: "# https://github.com/<owner>/<repo>[/path] ...".
func grammarURLComment(text string) bool {
	rest, ok := strings.CutPrefix(text, "# ")
	if !ok {
		if rest, ok = strings.CutPrefix(text, "// "); !ok {
			return false
		}
	}
	for _, url := range strings.Fields(rest) {
		if !grammarEvidence(`"`+url+`"`) || !strings.HasPrefix(url, "https://github.com/") {
			return false
		}
	}
	return rest != ""
}
