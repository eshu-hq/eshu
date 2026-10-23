// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package eval

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/ask/catalog"
	"github.com/eshu-hq/eshu/go/internal/mcp/contract/tool"
	"github.com/eshu-hq/eshu/go/internal/mcp/router"
)

// TopK is the candidate depth the harness asks for and scores. It is
// router.DefaultLimit, the count find_tool returns when a caller omits limit, so
// a change to that default changes the depth scored here.
const TopK = router.DefaultLimit

// Env is the registry view a ranker is evaluated against.
type Env struct {
	// Tools are the registered tool definitions the ranker chooses from.
	Tools []toolcontract.ToolDefinition
	// Entries maps a tool name to its catalog entry and may lack a tool.
	Entries map[string]catalog.Entry
	// Allowed reports whether the simulated caller may see a tool. It must be
	// non-nil; pass a func that returns true to evaluate the full registry.
	Allowed func(name string) bool
}

// Tally counts the questions in a group and how many the ranker got right.
type Tally struct {
	// Total is the number of questions.
	Total int
	// Top1 counts questions whose first candidate was acceptable.
	Top1 int
	// Top5 counts questions with an acceptable candidate in the first TopK.
	Top5 int
}

// Miss is one question whose first candidate was not acceptable.
type Miss struct {
	// ID, Family, and Question echo the corpus entry.
	ID       string
	Family   string
	Question string
	// Expected and Alternates are the acceptable tools.
	Expected   string
	Alternates []string
	// Got is the ranker's candidate names, best first, up to TopK.
	Got []string
	// Rank is the 1-based position of the first acceptable candidate, or 0 when
	// none appears in Got.
	Rank int
}

// Report is the outcome of one Run.
type Report struct {
	Tally
	// Families holds the same counts per family.
	Families map[string]Tally
	// Misses lists every top-1 miss in corpus order.
	Misses []Miss
}

// Run asks ranker each corpus question with Limit TopK and scores the answers.
// A question counts as top-1 when its first candidate is the expected tool or
// one of its alternates, and as top-5 when any of the first TopK candidates is.
// A ranker error aborts the run: a broken ranker is not an accuracy result.
func Run(ctx context.Context, ranker router.Ranker, c *Corpus, env Env) (*Report, error) {
	if ranker == nil {
		return nil, errors.New("eval: ranker is nil")
	}
	if c == nil {
		return nil, errors.New("eval: corpus is nil")
	}
	if env.Allowed == nil {
		return nil, errors.New("eval: Env.Allowed is nil")
	}
	rep := &Report{Families: map[string]Tally{}}
	for _, q := range c.Questions {
		cands, err := ranker.Rank(ctx, router.Query{
			Intent: q.Question, Anchors: q.Anchors.toRouter(), Limit: TopK,
		}, env.Tools, env.Entries, env.Allowed)
		if err != nil {
			return nil, fmt.Errorf("eval: rank %s: %w", q.ID, err)
		}
		if len(cands) > TopK {
			cands = cands[:TopK]
		}
		got := make([]string, len(cands))
		for i, cand := range cands {
			got[i] = cand.Name
		}
		rank := firstAcceptable(q, got)

		fam := rep.Families[q.Family]
		fam.Total++
		rep.Total++
		if rank >= 1 {
			fam.Top5++
			rep.Top5++
		}
		if rank == 1 {
			fam.Top1++
			rep.Top1++
		} else {
			rep.Misses = append(rep.Misses, Miss{
				ID: q.ID, Family: q.Family, Question: q.Question,
				Expected: q.Expected, Alternates: q.Alternates, Got: got, Rank: rank,
			})
		}
		rep.Families[q.Family] = fam
	}
	return rep, nil
}

// firstAcceptable returns the 1-based position of the first candidate that is
// the expected tool or an alternate, or 0 when none is.
func firstAcceptable(q Question, got []string) int {
	ok := map[string]bool{q.Expected: true}
	for _, alt := range q.Alternates {
		ok[alt] = true
	}
	for i, name := range got {
		if ok[name] {
			return i + 1
		}
	}
	return 0
}
