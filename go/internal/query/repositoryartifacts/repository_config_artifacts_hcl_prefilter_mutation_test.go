// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repositoryartifacts

import (
	"fmt"
	"reflect"
	"regexp"
	"regexp/syntax"
	"sync"
	"sync/atomic"
	"testing"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts/prefilter"
)

// hclScanRegexps lists the production scan patterns in prefilter order.
func hclScanRegexps() []*regexp.Regexp {
	return []*regexp.Regexp{
		terragruntDependencyConfigPathPattern,
		terragruntReadConfigPattern,
		terragruntIncludePathPattern,
		terragruntFindInParentFoldersPattern,
		localConfigFunctionStartPattern,
		localTerraformModuleSourcePattern,
	}
}

var hclScanNames = []string{
	"dependencyConfigPath", "readConfig", "includePath",
	"findInParentFolders", "configFunction", "moduleSource",
}

// syntaxMutation rewrites one node of a parsed pattern. site counts the
// applicable nodes met so far so each node becomes its own mutant.
type syntaxMutation struct {
	name  string
	apply func(n *syntax.Regexp) bool
}

func asciiOrbit(r rune) []rune {
	out := []rune{}
	hasNonASCII := false
	for c := unicode.SimpleFold(r); ; c = unicode.SimpleFold(c) {
		if c < 128 {
			out = append(out, c)
		} else {
			hasNonASCII = true
		}
		if c == r {
			break
		}
	}
	if !hasNonASCII {
		return nil
	}
	return out
}

func syntaxMutations() []syntaxMutation {
	return []syntaxMutation{
		{"ascii-only fold", func(n *syntax.Regexp) bool {
			if n.Op != syntax.OpLiteral || n.Flags&syntax.FoldCase == 0 {
				return false
			}
			parts := make([]*syntax.Regexp, 0, len(n.Rune))
			changed := false
			for _, r := range n.Rune {
				if members := asciiOrbit(r); members != nil {
					class := &syntax.Regexp{Op: syntax.OpCharClass}
					for _, m := range members {
						class.Rune = append(class.Rune, m, m)
					}
					parts = append(parts, class)
					changed = true
					continue
				}
				parts = append(parts, &syntax.Regexp{Op: syntax.OpLiteral, Rune: []rune{r}, Flags: n.Flags})
			}
			if !changed {
				return false
			}
			*n = syntax.Regexp{Op: syntax.OpConcat, Sub: parts}
			return true
		}},
		{"case-sensitive literal", func(n *syntax.Regexp) bool {
			if n.Op != syntax.OpLiteral || n.Flags&syntax.FoldCase == 0 {
				return false
			}
			lower := make([]rune, len(n.Rune))
			for i, r := range n.Rune {
				lower[i] = unicode.ToLower(r)
			}
			n.Rune = lower
			n.Flags &^= syntax.FoldCase
			return true
		}},
		{"invent word boundaries", func(n *syntax.Regexp) bool {
			if n.Op != syntax.OpLiteral {
				return false
			}
			lit := *n
			*n = syntax.Regexp{Op: syntax.OpConcat, Sub: []*syntax.Regexp{
				{Op: syntax.OpWordBoundary}, &lit, {Op: syntax.OpWordBoundary},
			}}
			return true
		}},
		{"require the optional part", func(n *syntax.Regexp) bool {
			if n.Op != syntax.OpQuest && n.Op != syntax.OpStar {
				return false
			}
			n.Op = syntax.OpPlus
			return true
		}},
		{"corrupt the literal", func(n *syntax.Regexp) bool {
			if n.Op != syntax.OpLiteral || len(n.Rune) == 0 {
				return false
			}
			n.Rune = append([]rune{'q'}, n.Rune[1:]...)
			return true
		}},
	}
}

// walk visits nodes in pre-order, calling f until it returns true.
func walk(n *syntax.Regexp, f func(*syntax.Regexp) bool) bool {
	if f(n) {
		return true
	}
	for _, s := range n.Sub {
		if walk(s, f) {
			return true
		}
	}
	return false
}

// mutateSite parses pattern, applies m at its k-th applicable node and
// returns the rendered pattern.
func mutateSite(pattern string, m syntaxMutation, k int) (string, bool) {
	tree, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return "", false
	}
	seen := 0
	done := walk(tree, func(n *syntax.Regexp) bool {
		// Probe on a copy so a failed apply never edits the tree.
		probe := *n
		if !m.apply(&probe) {
			return false
		}
		if seen == k {
			m.apply(n)
			return true
		}
		seen++
		return false
	})
	if !done {
		return "", false
	}
	return tree.String(), true
}

// dropAlternativeSites lists (pattern, branch) mutants separately because
// each alternative is its own claim, such as the terraform branch of the
// module-source scan.
func dropAlternative(pattern string, branch int) (string, bool) {
	tree, err := syntax.Parse(pattern, syntax.Perl)
	if err != nil {
		return "", false
	}
	var ok bool
	walk(tree, func(n *syntax.Regexp) bool {
		if n.Op != syntax.OpAlternate || branch >= len(n.Sub) || len(n.Sub) < 2 {
			return false
		}
		n.Sub = append(append([]*syntax.Regexp{}, n.Sub[:branch]...), n.Sub[branch+1:]...)
		if len(n.Sub) == 1 {
			*n = *n.Sub[0]
		}
		ok = true
		return true
	})
	if !ok {
		return "", false
	}
	return tree.String(), true
}

type hclMutantCase struct {
	scan    int
	name    string
	pattern string
}

// hclMutantCases enumerates every mutant whose derived gate differs from the
// production gate. A mutated pattern that derives the same gate is not a
// gate mutant and is not listed.
func hclMutantCases(t *testing.T) []hclMutantCase {
	t.Helper()
	res := hclScanRegexps()
	var out []hclMutantCase
	add := func(scan int, name, pattern string) {
		mutated, err := regexp.Compile(pattern)
		if err != nil {
			t.Fatalf("mutant %s/%s does not compile: %v", hclScanNames[scan], name, err)
		}
		base := prefilter.NewSet(res[scan]).Describe(0)
		if prefilter.NewSet(mutated).Describe(0) == base {
			return
		}
		out = append(out, hclMutantCase{scan: scan, name: name, pattern: pattern})
	}
	for scan, re := range res {
		for _, m := range syntaxMutations() {
			for k := 0; ; k++ {
				pattern, ok := mutateSite(re.String(), m, k)
				if !ok {
					break
				}
				add(scan, fmt.Sprintf("%s@%d", m.name, k), pattern)
			}
		}
		for branch := 0; branch < 4; branch++ {
			if pattern, ok := dropAlternative(re.String(), branch); ok {
				add(scan, fmt.Sprintf("drop alternative %d", branch), pattern)
			}
		}
	}
	return out
}

// pinnedEquivalentHCLMutants are the sweep mutants that no input can kill
// because the altered pattern accepts exactly the same language as the
// production pattern. Each is a word boundary inserted where the neighbouring
// pattern already forces a boundary, so the gate it derives is stricter on
// paper and identical in effect. The sweep checks each against a second
// corpus and fails when an entry stops surviving or an unlisted mutant
// survives. Keys are scan name / mutation name @ site index.
var pinnedEquivalentHCLMutants = map[string]string{
	"includePath/invent word boundaries@0":          `\b\bPATH\b\s*=: the leading \b is already in the pattern, and the byte after PATH is whitespace or "=", never a word character`,
	"configFunction/invent word boundaries@0":       `\b(?:\bFILE\b|templatefile)\(: the leading \b is already in the pattern, and "(" follows FILE, never a word character`,
	"configFunction/invent word boundaries@1":       `\b(?:file|\btemplatefile\b)\(: the leading \b is already in the pattern, and "(" follows TEMPLATEFILE, never a word character`,
	"dependencyConfigPath/invent word boundaries@0": `\bconfig_path\b: it is preceded by line start, CR, LF or \s (all non-word) and followed by \s*=`,
	"moduleSource/invent word boundaries@4":         `\b\bterraform\b\s*\{: the leading \b is already in the pattern, and \s or "{" follows terraform, never a word character`,
}

// TestHCLPrefilterMutantsAreKilled is the seeded-violation RED/GREEN pair
// for the wiring. GREEN: the production prefilters give identical rows over
// the corpus (the equality test). RED: each gate weakened in a way that makes
// it stricter than the pattern (an alternative dropped, the fold narrowed to
// ASCII, the flags flipped, a boundary invented, an optional part required)
// must change rows on the same corpus. The mutant is a prefilter derived from
// a deliberately altered pattern; the extractor and oracle still use the
// production patterns.
func TestHCLPrefilterMutantsAreKilled(t *testing.T) {
	t.Parallel()
	corpus := hclCorpus(1500, 31)
	want := make([][]map[string]any, len(corpus))
	for i, f := range corpus {
		want[i] = extractHCLConfigAssetRowsShipped("acme-repo", []querycontract.FileContent{f})
	}
	cases := hclMutantCases(t)
	if len(cases) < 20 {
		t.Fatalf("only %d gate mutants were generated; the sweep is not covering the gates", len(cases))
	}
	perScan := map[int]int{}
	var killed atomic.Int64
	var survivors sync.Map // mutant key -> true, for survivors identical to production on the second corpus
	t.Run("sweep", func(t *testing.T) {
		for _, c := range cases {
			perScan[c.scan]++
			key := hclScanNames[c.scan] + "/" + c.name
			t.Run(key, func(t *testing.T) {
				t.Parallel()
				res := hclScanRegexps()
				res[c.scan] = regexp.MustCompile(c.pattern)
				mutant := prefilter.NewSet(res...)
				for i, f := range corpus {
					got := extractHCLConfigAssetRowsWith(mutant, "acme-repo", []querycontract.FileContent{f})
					if !reflect.DeepEqual(got, want[i]) {
						killed.Add(1)
						return
					}
				}
				// A survivor must be indistinguishable from the production
				// pattern on a large independent corpus. A mutant that is
				// narrower on some file but changes no rows means the
				// corpus lacks the case.
				orig := hclScanRegexps()[c.scan]
				for _, f := range hclCorpus(4000, 977) {
					if !reflect.DeepEqual(orig.FindAllStringSubmatch(f.Content, -1), res[c.scan].FindAllStringSubmatch(f.Content, -1)) {
						t.Errorf("SURVIVED: mutant %s (pattern %s) is narrower than the production pattern on %q but changed no rows; add the case to the corpus",
							key, c.pattern, f.Content)
						return
					}
				}
				survivors.Store(key, true)
				if _, ok := pinnedEquivalentHCLMutants[key]; !ok {
					t.Errorf("UNPINNED SURVIVOR: mutant %s (pattern %s) changed no rows and matches production on the second corpus. "+
						"Prove it language-equivalent and pin it with its argument, or add the case that kills it", key, c.pattern)
					return
				}
				t.Logf("pinned equivalent: %s: %s", key, pinnedEquivalentHCLMutants[key])
			})
		}
	})
	// The pin list cannot rot: a pinned mutant that now dies, or no longer
	// exists, means its equivalence argument no longer describes the sweep.
	for key := range pinnedEquivalentHCLMutants {
		if _, ok := survivors.Load(key); !ok {
			t.Errorf("pinned equivalent mutant %s no longer survives (killed, or not generated); remove or re-derive its pin", key)
		}
	}
	nSurvivors := 0
	survivors.Range(func(_, _ any) bool { nSurvivors++; return true })
	t.Logf("gate mutants=%d killed=%d equivalent=%d", len(cases), killed.Load(), nSurvivors)
	for scan, name := range hclScanNames {
		if perScan[scan] == 0 {
			t.Errorf("no mutants for scan %s; its gate is not under test", name)
		}
	}
}

// TestHCLPrefilterNamedMutantsAreKilled names the mutants the review brief
// asks for, so each is killed by an explicitly named case and not only by the
// enumerated sweep.
func TestHCLPrefilterNamedMutantsAreKilled(t *testing.T) {
	t.Parallel()
	module := localTerraformModuleSourcePattern.String()
	killers := []struct {
		name    string
		scan    int
		pattern func() (string, bool)
		file    string
		path    string
	}{
		{
			name: "module-source without the terraform branch",
			scan: hclScanModuleSource,
			pattern: func() (string, bool) {
				return dropAlternative(module, 1)
			},
			file: "terraform {\n  source = \"../mods//x\"\n}\n",
			path: "terragrunt.hcl",
		},
		{
			name: "module-source without the module branch",
			scan: hclScanModuleSource,
			pattern: func() (string, bool) {
				return dropAlternative(module, 0)
			},
			file: "module \"a\" {\n  source = \"./a\"\n}\n",
			path: "main.tf",
		},
		{
			name: "module-source ASCII-only fold misses LONG S",
			scan: hclScanModuleSource,
			pattern: func() (string, bool) {
				return mutateSite(module, syntaxMutations()[0], 0)
			},
			file: "module \"a\" {\n  x\u017fource = \"./a\"\n}\n",
			path: "main.tf",
		},
		{
			name: "find_in_parent_folders ASCII-only fold misses LONG S",
			scan: hclScanFindInParentFolders,
			pattern: func() (string, bool) {
				return mutateSite(terragruntFindInParentFoldersPattern.String(), syntaxMutations()[0], 0)
			},
			file: "x = find_in_parent_folder\u017f(\"a.hcl\")\n",
			path: "terragrunt.hcl",
		},
		{
			name: "config_path flags flipped misses upper case",
			scan: hclScanDependencyConfigPath,
			pattern: func() (string, bool) {
				return mutateSite(terragruntDependencyConfigPathPattern.String(), syntaxMutations()[1], 0)
			},
			file: "dependency \"a\" {\n  CONFIG_PATH = \"../a\"\n}\n",
			path: "terragrunt.hcl",
		},
		{
			name: "function start without templatefile alternative",
			scan: hclScanConfigFunction,
			pattern: func() (string, bool) {
				return dropAlternative(localConfigFunctionStartPattern.String(), 0)
			},
			file: "x = file(\"a/b.yaml\")\n",
			path: "main.tf",
		},
		{
			name: "read config without the find_in_parent_folders branch",
			scan: hclScanReadConfig,
			pattern: func() (string, bool) {
				return dropAlternative(terragruntReadConfigPattern.String(), 0)
			},
			file: "x = read_terragrunt_config(find_in_parent_folders())\n",
			path: "terragrunt.hcl",
		},
	}
	for _, k := range killers {
		t.Run(k.name, func(t *testing.T) {
			t.Parallel()
			pattern, ok := k.pattern()
			if !ok {
				t.Fatal("mutation did not apply")
			}
			res := hclScanRegexps()
			res[k.scan] = regexp.MustCompile(pattern)
			files := []querycontract.FileContent{{RelativePath: k.path, Content: k.file}}
			want := extractHCLConfigAssetRowsShipped("r", files)
			if len(want) == 0 {
				t.Fatal("the killing file must produce rows under the shipped extractor")
			}
			if got := extractHCLConfigAssetRowsWith(prefilter.NewSet(res...), "r", files); reflect.DeepEqual(got, want) {
				t.Fatalf("mutant survived on %q: rows %v", k.file, got)
			}
			if got := extractHCLConfigAssetRows("r", files); !reflect.DeepEqual(got, want) {
				t.Fatalf("production gates changed rows on %q: %v vs %v", k.file, got, want)
			}
		})
	}
}
