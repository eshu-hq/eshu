// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package prefilter

import (
	"regexp"
	"testing"
	"unicode/utf8"
)

// mutant is a deliberate corruption of a derived gate. Every mutant is
// stricter than the truth somewhere, so a sound test harness must report a
// violation for it. A mutant the harness cannot kill means the harness has a
// blind spot.
type mutant struct {
	name  string
	apply func(e *expr) (*expr, bool)
}

// mapExpr rebuilds e bottom-up, letting f rewrite each node. changed reports
// whether any rewrite fired.
func mapExpr(e *expr, f func(*expr) (*expr, bool)) (out *expr, changed bool) {
	cp := *e
	cp.subs = nil
	for _, s := range e.subs {
		m, c := mapExpr(s, f)
		cp.subs = append(cp.subs, m)
		changed = changed || c
	}
	if r, c := f(&cp); c {
		return r, true
	}
	return &cp, changed
}

func mapLiterals(g func(literal) (literal, bool)) func(*expr) (*expr, bool) {
	return func(e *expr) (*expr, bool) {
		if e.kind != kindLit {
			return e, false
		}
		l, ok := g(e.lit)
		if !ok {
			return e, false
		}
		e.lit = l
		return e, true
	}
}

func mutants() []mutant {
	return []mutant{
		{"ascii-only fold drops the special folds", func(e *expr) (*expr, bool) {
			return mapExpr(e, mapLiterals(func(l literal) (literal, bool) {
				changed := false
				elems := make([][]rune, len(l.elems))
				for i, o := range l.elems {
					var ascii []rune
					for _, r := range o {
						if r < utf8.RuneSelf {
							ascii = append(ascii, r)
						}
					}
					if len(ascii) == 0 || len(ascii) == len(o) {
						elems[i] = o
						continue
					}
					elems[i] = ascii
					changed = true
				}
				l.elems = elems
				return l, changed
			}))
		}},
		{"case-sensitive literal (flags flipped)", func(e *expr) (*expr, bool) {
			return mapExpr(e, mapLiterals(func(l literal) (literal, bool) {
				changed := false
				elems := make([][]rune, len(l.elems))
				for i, o := range l.elems {
					keep := o[0]
					for _, r := range o {
						if r >= 'a' && r <= 'z' {
							keep = r
						}
					}
					elems[i] = []rune{keep}
					changed = changed || len(o) > 1
				}
				l.elems = elems
				return l, changed
			}))
		}},
		{"drop the last alternative", func(e *expr) (*expr, bool) {
			return mapExpr(e, func(n *expr) (*expr, bool) {
				if n.kind != kindOr || len(n.subs) < 2 {
					return n, false
				}
				n.subs = n.subs[:len(n.subs)-1]
				if len(n.subs) == 1 {
					return n.subs[0], true
				}
				return n, true
			})
		}},
		{"drop the first alternative", func(e *expr) (*expr, bool) {
			return mapExpr(e, func(n *expr) (*expr, bool) {
				if n.kind != kindOr || len(n.subs) < 2 {
					return n, false
				}
				n.subs = n.subs[1:]
				if len(n.subs) == 1 {
					return n.subs[0], true
				}
				return n, true
			})
		}},
		{"require every alternative", func(e *expr) (*expr, bool) {
			return mapExpr(e, func(n *expr) (*expr, bool) {
				if n.kind != kindOr {
					return n, false
				}
				n.kind = kindAnd
				return n, true
			})
		}},
		{"invent word boundaries", func(e *expr) (*expr, bool) {
			return mapExpr(e, mapLiterals(func(l literal) (literal, bool) {
				if l.before && l.after {
					return l, false
				}
				l.before, l.after = true, true
				return l, true
			}))
		}},
		{"ignore the orbit tail of one element", func(e *expr) (*expr, bool) {
			return mapExpr(e, mapLiterals(func(l literal) (literal, bool) {
				changed := false
				elems := make([][]rune, len(l.elems))
				copy(elems, l.elems)
				for i, o := range elems {
					if len(o) > 2 {
						elems[i] = o[:2]
						changed = true
					}
				}
				l.elems = elems
				return l, changed
			}))
		}},
	}
}

// TestHarnessKillsEveryMutantAndPassesTheControl is the seeded-violation
// RED/GREEN pair. The control (the derived gates, unmutated) must produce no
// violation; each mutant, applied to the same derived gates, must.
func TestHarnessKillsEveryMutantAndPassesTheControl(t *testing.T) {
	t.Parallel()
	patterns := soundnessPatterns()
	res := compileAll(t, patterns)
	inputs := generateInputs(patterns, 120, 4242)

	control := NewSet(res...)
	if bad, _ := checkSound(control, res, inputs); len(bad) > 0 {
		t.Fatalf("control must be clean, got %d violations, first: %s", len(bad), bad[0])
	}

	for _, m := range mutants() {
		t.Run(m.name, func(t *testing.T) {
			t.Parallel()
			roots := make([]*expr, len(res))
			mutated := 0
			for i, re := range res {
				root, err := derive(re.String())
				if err != nil {
					t.Fatalf("derive %q: %v", re.String(), err)
				}
				if r, changed := m.apply(root); changed {
					root = r
					mutated++
				}
				roots[i] = root
			}
			if mutated == 0 {
				t.Fatalf("mutant %q changed no pattern; it cannot be killed and proves nothing", m.name)
			}
			bad, _ := checkSound(newSetFromRoots(roots), res, inputs)
			if len(bad) == 0 {
				t.Fatalf("mutant %q survived: %d patterns mutated, no violation found", m.name, mutated)
			}
			t.Logf("mutant %q killed: %d violations over %d mutated patterns, e.g. %s", m.name, len(bad), mutated, bad[0])
		})
	}
}

// TestMutationHarnessSeesAllSpecialFolds makes the ascii-only mutant's kill
// depend on KELVIN SIGN and LONG S specifically, not on incidental noise.
func TestMutationHarnessSeesAllSpecialFolds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ pattern, input string }{
		{`(?i)kelvin`, "\u212aelvin"},
		{`(?i)source`, "\u017fource"},
	} {
		re := regexp.MustCompile(tc.pattern)
		root, err := derive(tc.pattern)
		if err != nil {
			t.Fatal(err)
		}
		mut, changed := mutants()[0].apply(root)
		if !changed {
			t.Fatalf("ascii-only mutant did not change %q", tc.pattern)
		}
		bad, _ := checkSound(newSetFromRoots([]*expr{mut}), []*regexp.Regexp{re}, []string{tc.input})
		if len(bad) != 1 {
			t.Fatalf("ascii-only mutant must be killed by %q on %q", tc.input, tc.pattern)
		}
	}
}
