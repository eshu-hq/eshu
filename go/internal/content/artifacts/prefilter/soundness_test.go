// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prefilter

import (
	"fmt"
	"math/rand"
	"regexp"
	"regexp/syntax"
	"strings"
	"testing"
	"unicode/utf8"
)

// soundnessPatterns are shape fixtures for the engine: the syntax shapes the
// HCL extractor uses plus the cases where folding, boundaries, alternation
// and size caps matter. They are inputs to the property test, not a claim
// about any consumer's patterns (the consumer pins its own).
func soundnessPatterns() []string {
	return []string{
		`(?im)(?:^|[\r\n])\s*config_path\s*=\s*(.+?)\s*(?:$|[\r\n])`,
		`(?i)read_terragrunt_config\(\s*(?:find_in_parent_folders\(\s*(?:"([^"]+)")?\s*\)|"([^"]+)")`,
		`(?i)find_in_parent_folders\(\s*(?:"([^"]+)")?\s*\)`,
		`(?i)\bpath\s*=\s*find_in_parent_folders\(\s*(?:"([^"]+)")?\s*\)`,
		`(?i)\b(?:file|templatefile)\(`,
		`(?im)(?:module\s+"[^"]+"\s*\{[^}]*?|\bterraform\s*\{[^}]*?)\bsource\b\s*=\s*(.+?)\s*(?:$|[\r\n])`,
		`(?i)kelvin`, `(?i)ks`, `(?i)\x{17f}`, `(?i)caf\x{e9}`, `(?i)\x{1c6}x`,
		`(?i)\bsource\b`, `\bsource\b`, `source\b`, `\bsource`, `\Bsource`,
		`foo|bar`, `(?i)foo|bar|baz`, `a*b?c`, `(?:ab)+c`, `x{2,3}y`, `[kK]ey`, `(?i)[k]ey`,
		`^foo$`, `(?m)^foo$`, `(?s)a.*b`, `a.b`, `[a-z]+`, ``, `\x{FFFD}`, `a\x{FFFD}b`,
		`(?:aa|bb|cc|dd|ee)(?:ff|gg|hh|ii|jj)x`, `(?:a|b)(?:c|d)(?:e|f)(?:g|h)(?:i|j)`,
		`foo(?:bar)?baz`, `(foo)(bar)`, `(?:foo|)bar`, `\bfoo\b|\bbar\b`, `(?i)\b(?:a|b)\b`,
		`[^a]b`, `\w+_path`, `foo\s+"[^"]+"\s*\{`, `(?i)terraform\s*\{`,
	}
}

// noise is the pool of runes and byte fragments mixed into generated text.
var noise = []string{
	" ", "\t", "\n", "\r\n", "\x00", "_", "-", "=", "{", "}", "(", ")", "\"", "/", "0", "9",
	"a", "Z", "x", "\u00e9", "\u00c9", "\u212a", "\u017f", "\u0130", "\u0131", "\u01c5", "\ufffd",
	"\xff", "\xc5", "\xe2\x82", "\xf0\x9f", "#", "//", "locals", "resource", "module \"m\" {",
}

// sampleMatch writes a random string that tends to match re, picking a random
// fold variant for case-insensitive literals so the special folds appear.
func sampleMatch(re *syntax.Regexp, rng *rand.Rand, b *strings.Builder) {
	switch re.Op {
	case syntax.OpLiteral:
		fold := re.Flags&syntax.FoldCase != 0
		for _, r := range re.Rune {
			o := orbit(r, fold)
			b.WriteRune(o[rng.Intn(len(o))])
		}
	case syntax.OpCharClass:
		if len(re.Rune) == 0 {
			return
		}
		pair := rng.Intn(len(re.Rune)/2) * 2
		lo, hi := re.Rune[pair], re.Rune[pair+1]
		span := int(hi-lo) + 1
		if span > 4096 {
			span = 4096
		}
		b.WriteRune(lo + rune(rng.Intn(span)))
	case syntax.OpAnyChar, syntax.OpAnyCharNotNL:
		b.WriteString(noise[rng.Intn(len(noise))])
	case syntax.OpCapture:
		sampleMatch(re.Sub[0], rng, b)
	case syntax.OpStar:
		for i := rng.Intn(3); i > 0; i-- {
			sampleMatch(re.Sub[0], rng, b)
		}
	case syntax.OpPlus:
		for i := 1 + rng.Intn(3); i > 0; i-- {
			sampleMatch(re.Sub[0], rng, b)
		}
	case syntax.OpQuest:
		if rng.Intn(2) == 0 {
			sampleMatch(re.Sub[0], rng, b)
		}
	case syntax.OpRepeat:
		n := re.Min
		if re.Max > re.Min {
			n += rng.Intn(re.Max - re.Min + 1)
		}
		for ; n > 0; n-- {
			sampleMatch(re.Sub[0], rng, b)
		}
	case syntax.OpConcat:
		for _, sub := range re.Sub {
			sampleMatch(sub, rng, b)
		}
	case syntax.OpAlternate:
		sampleMatch(re.Sub[rng.Intn(len(re.Sub))], rng, b)
	case syntax.OpBeginLine, syntax.OpEndLine:
		if rng.Intn(2) == 0 {
			b.WriteByte('\n')
		}
	}
}

// edit perturbs text so near misses (a literal cut in two, a boundary
// broken, a fold variant swapped) are as common as matches.
func edit(text string, rng *rand.Rand) string {
	n := 1 + rng.Intn(3)
	for ; n > 0; n-- {
		switch rng.Intn(8) {
		case 0: // insert noise at a random offset
			at := rng.Intn(len(text) + 1)
			text = text[:at] + noise[rng.Intn(len(noise))] + text[at:]
		case 1: // delete a byte
			if len(text) > 0 {
				at := rng.Intn(len(text))
				text = text[:at] + text[at+1:]
			}
		case 2: // truncate
			text = text[:rng.Intn(len(text)+1)]
		case 3: // prefix a word character
			text = "a" + text
		case 4: // suffix a word character
			text += "_"
		case 5: // swap case of ASCII letters
			text = strings.Map(func(r rune) rune {
				if rng.Intn(3) == 0 && r >= 'a' && r <= 'z' {
					return r - 32
				}
				if rng.Intn(3) == 0 && r >= 'A' && r <= 'Z' {
					return r + 32
				}
				return r
			}, text)
		case 6: // swap k and s for their special folds
			text = strings.NewReplacer("k", "\u212a", "s", "\u017f").Replace(text)
		case 7: // CRLF line endings
			text = strings.ReplaceAll(text, "\n", "\r\n")
		}
	}
	return text
}

type violation struct {
	pattern string
	input   string
	gate    string
}

func (v violation) String() string {
	return fmt.Sprintf("pattern %q gate %s rejects matching input %q", v.pattern, v.gate, v.input)
}

// checkSound runs every input against every pattern and returns the cases
// where the gate said no and Go's matcher said yes, plus per-pattern counts
// of matching inputs (the vacuity guard).
func checkSound(set *Set, res []*regexp.Regexp, inputs []string) (bad []violation, matched []int) {
	matched = make([]int, len(res))
	for _, in := range inputs {
		gates := set.Evaluate(in)
		for i, re := range res {
			if !re.MatchString(in) {
				continue
			}
			matched[i]++
			if !gates[i] {
				bad = append(bad, violation{pattern: re.String(), input: in, gate: set.Describe(i)})
			}
		}
	}
	return bad, matched
}

// generateInputs builds a corpus mixing sampled matches, edited matches, and
// pure noise for the supplied patterns.
func generateInputs(patterns []string, perPattern int, seed int64) []string {
	rng := rand.New(rand.NewSource(seed))
	var out []string
	for _, p := range patterns {
		parsed, err := syntax.Parse(p, syntax.Perl)
		if err != nil {
			continue
		}
		for i := 0; i < perPattern; i++ {
			var b strings.Builder
			if rng.Intn(3) == 0 {
				b.WriteString(noise[rng.Intn(len(noise))])
			}
			sampleMatch(parsed, rng, &b)
			if rng.Intn(3) == 0 {
				b.WriteString(noise[rng.Intn(len(noise))])
			}
			text := b.String()
			out = append(out, text, edit(text, rng))
		}
	}
	for i := 0; i < perPattern; i++ {
		var b strings.Builder
		for n := rng.Intn(24); n > 0; n-- {
			b.WriteString(noise[rng.Intn(len(noise))])
		}
		out = append(out, b.String())
	}
	return out
}

func compileAll(t testing.TB, patterns []string) []*regexp.Regexp {
	t.Helper()
	res := make([]*regexp.Regexp, 0, len(patterns))
	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			t.Fatalf("fixture pattern %q does not compile: %v", p, err)
		}
		res = append(res, re)
	}
	return res
}

// TestGateIsSoundOverFixturePatterns is the soundness property: for every
// fixture pattern and every generated input, gate false implies no match.
func TestGateIsSoundOverFixturePatterns(t *testing.T) {
	t.Parallel()
	patterns := soundnessPatterns()
	res := compileAll(t, patterns)
	set := NewSet(res...)
	per := 150
	if testing.Short() {
		per = 40
	}
	inputs := generateInputs(patterns, per, 7126)
	bad, matched := checkSound(set, res, inputs)
	for _, v := range bad {
		t.Errorf("UNSOUND: %s", v)
		if len(bad) > 5 {
			break
		}
	}
	for i, p := range patterns {
		if matched[i] == 0 && p != `\x{FFFD}` && p != `a\x{FFFD}b` {
			t.Errorf("pattern %q never matched a generated input; the property is vacuous for it", p)
		}
	}
}

// TestGateIsSoundOverRandomPatterns derives gates for patterns drawn from a
// small grammar so the analysis is exercised on shapes nobody wrote by hand.
func TestGateIsSoundOverRandomPatterns(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(20261010))
	var patterns []string
	for len(patterns) < 300 {
		p := randomPattern(rng, 3)
		if _, err := regexp.Compile(p); err == nil {
			patterns = append(patterns, p)
		}
	}
	res := compileAll(t, patterns)
	for i, re := range res {
		set := NewSet(re)
		inputs := generateInputs([]string{re.String()}, 25, int64(i))
		bad, _ := checkSound(set, []*regexp.Regexp{re}, inputs)
		for _, v := range bad {
			t.Errorf("UNSOUND: %s", v)
			break
		}
	}
}

var (
	grammarWords = []string{"source", "module", "terraform", "file", "k", "ks", "ab", "a", "b", "=", "{", "(", "path", "x_y", "\u00e9", "\u017f"}
	grammarAtoms = []string{`\s*`, `\s+`, `.`, `.*?`, `[^}]*?`, `[ab]`, `[a-z]`, `\b`, `\B`, `\w`, `"[^"]+"`, `[\r\n]`}
	grammarFlags = []string{``, `(?i)`, `(?im)`, `(?s)`, `(?m)`}
)

func randomPattern(rng *rand.Rand, depth int) string {
	return grammarFlags[rng.Intn(len(grammarFlags))] + randomNode(rng, depth)
}

func randomNode(rng *rand.Rand, depth int) string {
	if depth == 0 {
		return randomLeaf(rng)
	}
	switch rng.Intn(9) {
	case 0, 1:
		return randomLeaf(rng)
	case 2, 3, 4:
		n := 2 + rng.Intn(3)
		parts := make([]string, n)
		for i := range parts {
			parts[i] = randomNode(rng, depth-1)
		}
		return strings.Join(parts, "")
	case 5:
		return "(?:" + randomNode(rng, depth-1) + "|" + randomNode(rng, depth-1) + ")"
	case 6:
		return "(?:" + randomNode(rng, depth-1) + ")?"
	case 7:
		return "(?:" + randomNode(rng, depth-1) + ")+"
	default:
		return "(" + randomNode(rng, depth-1) + ")"
	}
}

func randomLeaf(rng *rand.Rand) string {
	if rng.Intn(4) == 0 {
		return grammarAtoms[rng.Intn(len(grammarAtoms))]
	}
	return regexp.QuoteMeta(grammarWords[rng.Intn(len(grammarWords))])
}

// FuzzGateIsSound lets `go test -fuzz` search for an unsound gate. The seed
// corpus runs on every plain `go test`.
func FuzzGateIsSound(f *testing.F) {
	patterns := soundnessPatterns()
	res := compileAll(f, patterns)
	set := NewSet(res...)
	for _, in := range generateInputs(patterns, 3, 1) {
		f.Add(in)
	}
	f.Add("module \"a\" {\n  \u017fource = \"./x\"\n}\n")
	f.Add("\xff\xfe terraform {\r\n source = \"./m\"\r\n}")
	f.Fuzz(func(t *testing.T, in string) {
		if !utf8.ValidString(in) && len(in) > 1<<16 {
			t.Skip()
		}
		bad, _ := checkSound(set, res, []string{in})
		if len(bad) > 0 {
			t.Fatalf("UNSOUND: %s", bad[0])
		}
	})
}
