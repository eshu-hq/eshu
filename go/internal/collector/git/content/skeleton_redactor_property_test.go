// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
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
}

var skeletonKeys = []string{"region", "db_password", "api_token", "client_secret", "stripe", "tags", "Name", "value", "conn"}

// skeletonLine renders one random raw line shape around a seed. Multi-line
// shapes (heredoc, PEM, block comment, nested block) return several lines.
func skeletonLine(rng *rand.Rand, seed string) []string {
	key := skeletonKeys[rng.IntN(len(skeletonKeys))]
	indent := strings.Repeat(" ", rng.IntN(3)*2)
	switch rng.IntN(20) {
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
	case 18:
		// Noise the allowlist keeps.
		return []string{indent + key + " = " + []string{"true", "3", "-1.5", "var.x", "local.y", "module.m.id", "\"1.2.3\"", "\"~> 5.0\"", "null"}[rng.IntN(9)]}
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
		kind := rawFindingKind(raw)
		switch {
		case stored == raw:
		case kind != "" && !strings.Contains(stored, markerPrefix+kind):
			t.Fatalf("line %d: raw finding %s lost: %q -> %q", i+1, kind, raw, stored)
		case kind == "" && holdsKindMarker(stored):
			t.Fatalf("line %d: no raw finding but a kind marker was added: %q -> %q", i+1, raw, stored)
		}
	}
}

// rawFindingKind is migration 138 on one line: the line gate, then the CASE.
func rawFindingKind(line string) string {
	if len(detect.Spans(line)) == 0 {
		return ""
	}
	return detect.Classify(line)
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
		if trimmed == "" || isGrammarMarker(trimmed) || slices.Contains([]string{"{", "}", "[", "]", "},", "],", "{}", "[]", "{},", "[],"}, trimmed) {
			continue
		}
		value, ok := grammarValue(trimmed)
		if !ok {
			return fmt.Errorf("line %d %q is outside the skeleton grammar", i+1, line)
		}
		value = strings.TrimSuffix(value, ",")
		switch {
		case value == "{" || value == "[" || value == "{}" || value == "[]":
		case strings.HasPrefix(value, "<<"):
			heredoc = strings.TrimPrefix(strings.TrimPrefix(value, "<<"), "-")
		case isGrammarMarker(value) || grammarAllowed(value):
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
		_, err := strconv.ParseFloat(value, 64)
		return err == nil
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
		for _, planted := range []string{
			in + "\npw = \"QqZzSeed9988\"\n",
			in + "\n# QqZzSeed9988\n",
			in + "\n\"QqZzSeed9988\",\n",
			in + "\nx = <<EOT\nQqZzSeed9988\nEOT\n",
		} {
			out, _, err := redactSkeleton(planted)
			if err == nil && strings.Contains(out, "QqZzSeed9988") {
				t.Fatalf("planted seed survived: in=%q out=%q", planted, out)
			}
		}
	})
}
