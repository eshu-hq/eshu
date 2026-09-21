// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// TokenFloor is the minimum token count a function needs to join a finding.
// It matches the #6834 floor recommendation (default 50): below it, buckets
// are scaffold noise, not actionable clones. Rows below the floor are
// dropped before assembly and counted under the below_floor rule.
const TokenFloor = 50

// WrapperFamilyMinMembers is the smallest same-name, multi-package group
// that counts as an intentional parallel family (the per-service wrapper
// class from #6834 §4: recordAPICall ×130). Genuine small clones (×3
// services) stay below it and keep reporting.
const WrapperFamilyMinMembers = 5

// LargeBodyTokens marks bodies worth a zero-weight large_body signal reason:
// at or above it, a finding's copies are large enough that a maintainer
// judges them differently from small utilities.
const LargeBodyTokens = 200

// Kind is a finding kind: exact token-stream equality, alpha-renamed
// equality, reducer-verified Jaccard drift, or graph-qualified wrapper
// bypass. Drifted findings assemble in drifted.go from
// reducer_code_drifted_finding facts, never from fingerprint groups;
// wrapper-bypass findings assemble in wrapper_bypass.go from one qualified
// target per nominated wrapper family.
type Kind string

const (
	// KindExact groups members sharing one fp_exact hash: identical token
	// streams, the strongest parallel-implementation signal.
	KindExact Kind = "parallel_implementation.exact"
	// KindRenamed groups members sharing one fp_renamed hash: identical
	// streams up to identifier renaming.
	KindRenamed Kind = "parallel_implementation.renamed"
)

// Reason codes. Value-bearing reasons decompose the score without remainder;
// zero-weight signal reasons name judgment signals that carry no score
// weight, listed so there are no hidden terms.
const (
	ReasonIdenticalStream = "identical_token_stream"
	ReasonRenamedStream   = "renamed_token_stream"
	ReasonAdditionalCopy  = "additional_copy"
	ReasonSpanPackages    = "members_span_packages"
	ReasonLargeBody       = "body_tokens_large"
)

// Suppression rule names, reported per rule in every response so a quiet
// result is distinguishable from a filtered one.
const (
	RuleBelowFloor      = "below_floor"
	RuleGenerated       = "generated_file"
	RuleVendored        = "vendored_path"
	RuleTestFile        = "test_file"
	RuleTrivialAccessor = "trivial_accessor"
	RuleWrapperFamily   = "wrapper_family"
)

// Member is one function in a fingerprint equality group.
type Member struct {
	EntityID     string
	EntityName   string
	EntityType   string
	RelativePath string
	Language     string
	StartLine    int
	EndLine      int
	TokenCount   int
}

// Reason is one scored or signal component of a finding. Score always equals
// the sum of its reasons' values.
type Reason struct {
	Code     string `json:"code"`
	Sentence string `json:"sentence"`
	Value    int    `json:"value"`
}

// Finding is one parallel-implementation group that survived suppression.
// Confidence carries the weakest contributing CALLS-edge confidence for
// graph-derived kinds (wrapper_bypass); it is zero and omitted for the
// content-index kinds, which have no edge evidence.
type Finding struct {
	ID           string         `json:"finding_id"`
	RepoID       string         `json:"repo_id"`
	Kind         Kind           `json:"kind"`
	Fingerprint  string         `json:"fingerprint"`
	Members      []Member       `json:"members"`
	Reasons      []Reason       `json:"reasons"`
	Score        int            `json:"score"`
	Confidence   float64        `json:"confidence,omitempty"`
	Suppressions map[string]int `json:"suppressions"`
}

// findingID derives a stable id from (repo_id, kind, fingerprint) so a
// finding keeps its id across generations while the clone class exists.
func findingID(repoID string, kind Kind, fingerprint string) string {
	sum := sha256.Sum256([]byte(repoID + "\x00" + string(kind) + "\x00" + fingerprint))
	return hex.EncodeToString(sum[:])[:16]
}

// PackageOf returns the parent directory of a relative path: members in
// different directories span packages. It doubles as the owning-package
// signal investigate reports per member.
func PackageOf(relativePath string) string {
	trimmed := strings.Trim(relativePath, "/")
	if index := strings.LastIndex(trimmed, "/"); index >= 0 {
		return trimmed[:index]
	}
	return "."
}

// suppressMembers applies the member-level suppression catalogue shared by
// every finding kind: token floor, generated, vendored, test files (unless
// opted back in), and trivial accessors. It returns the survivors with the
// per-rule counts, so a quiet result stays distinguishable from a filtered
// one at every call site.
func suppressMembers(members []Member, includeTests bool) ([]Member, map[string]int) {
	suppressions := map[string]int{}
	survivors := make([]Member, 0, len(members))
	for _, member := range members {
		if member.TokenCount < TokenFloor {
			suppressions[RuleBelowFloor]++
			continue
		}
		if SuppressGenerated(member) {
			suppressions[RuleGenerated]++
			continue
		}
		if SuppressVendored(member) {
			suppressions[RuleVendored]++
			continue
		}
		if SuppressTestFile(member, includeTests) {
			suppressions[RuleTestFile]++
			continue
		}
		if SuppressTrivialAccessor(member) {
			suppressions[RuleTrivialAccessor]++
			continue
		}
		survivors = append(survivors, member)
	}
	return survivors, suppressions
}

// AssembleFinding builds the finding for one fingerprint group, applying
// member-level suppression first. It reports false when fewer than two
// members survive: a single surviving copy is not a parallel
// implementation. Suppressions counts every rule application, including the
// token floor.
func AssembleFinding(repoID string, kind Kind, fingerprint string, members []Member, includeTests bool) (Finding, bool) {
	survivors, suppressions := suppressMembers(members, includeTests)
	if SuppressWrapperFamily(survivors) {
		suppressions[RuleWrapperFamily] += len(survivors)
		survivors = nil
	}
	// Suppression counts report even when the group drops below two
	// survivors: a quiet result must stay distinguishable from a filtered
	// one, so AssemblePage merges these counts from dropped groups too.
	partial := Finding{
		ID:           findingID(repoID, kind, fingerprint),
		RepoID:       repoID,
		Kind:         kind,
		Fingerprint:  fingerprint,
		Suppressions: suppressions,
	}
	if len(survivors) < 2 {
		return partial, false
	}
	tokenCount := 0
	for _, member := range survivors {
		if member.TokenCount > tokenCount {
			tokenCount = member.TokenCount
		}
	}
	score := len(survivors) * tokenCount
	reasons := buildReasons(kind, len(survivors), tokenCount, survivors)
	return Finding{
		ID:           findingID(repoID, kind, fingerprint),
		RepoID:       repoID,
		Kind:         kind,
		Fingerprint:  fingerprint,
		Members:      survivors,
		Reasons:      reasons,
		Score:        score,
		Suppressions: suppressions,
	}, true
}

// buildReasons decomposes members × tokens without remainder: one stream
// reason plus one additional-copy reason per extra member, then zero-weight
// signal reasons for package span and large bodies.
func buildReasons(kind Kind, memberCount, tokenCount int, members []Member) []Reason {
	streamCode := ReasonIdenticalStream
	streamSentence := fmt.Sprintf("identical token stream across %d members (%d tokens each)", memberCount, tokenCount)
	if kind == KindRenamed {
		streamCode = ReasonRenamedStream
		streamSentence = fmt.Sprintf("identical token stream up to identifier renaming across %d members (%d tokens each)", memberCount, tokenCount)
	}
	reasons := []Reason{{Code: streamCode, Sentence: streamSentence, Value: tokenCount}}
	for i := 1; i < memberCount; i++ {
		reasons = append(reasons, Reason{
			Code:     ReasonAdditionalCopy,
			Sentence: fmt.Sprintf("additional identical copy %d of %d (%d tokens)", i+1, memberCount, tokenCount),
			Value:    tokenCount,
		})
	}
	packages := map[string]struct{}{}
	for _, member := range members {
		packages[PackageOf(member.RelativePath)] = struct{}{}
	}
	if len(packages) > 1 {
		reasons = append(reasons, Reason{
			Code:     ReasonSpanPackages,
			Sentence: fmt.Sprintf("members span %d packages (ranking signal, no score weight)", len(packages)),
			Value:    0,
		})
	}
	if tokenCount >= LargeBodyTokens {
		reasons = append(reasons, Reason{
			Code:     ReasonLargeBody,
			Sentence: fmt.Sprintf("body holds %d tokens at or above the %d-token large-body mark (judgment signal, no score weight)", tokenCount, LargeBodyTokens),
			Value:    0,
		})
	}
	return reasons
}

// Group is one fingerprint-equality group as read from the store, before
// suppression and finding assembly.
type Group struct {
	Fingerprint string
	Members     []Member
}

// GroupStat is one phase-one grouping row: the fingerprint, its member
// count, and the max token count. The score (members × tokens) derives
// without hydrating members, so merged cross-kind paging sorts the full
// group list before any member fetch.
type GroupStat struct {
	Kind        Kind
	Fingerprint string
	Members     int
	Tokens      int
}

// StatID returns the finding id a stat will carry once assembled: the same
// (repo_id, kind, fingerprint) derivation, so stat order and finding order
// agree exactly on score ties.
func StatID(repoID string, kind Kind, fingerprint string) string {
	return findingID(repoID, kind, fingerprint)
}

// StatScore returns the members × tokens score a stat's finding will carry.
func StatScore(stat GroupStat) int {
	return stat.Members * stat.Tokens
}

// PageStats sorts stats by score desc, finding id asc and returns the
// offset/limit window in final response order.
func PageStats(repoID string, stats []GroupStat, offset, limit int) []GroupStat {
	ranked := append([]GroupStat(nil), stats...)
	sort.Slice(ranked, func(i, j int) bool {
		scoreI, scoreJ := StatScore(ranked[i]), StatScore(ranked[j])
		if scoreI != scoreJ {
			return scoreI > scoreJ
		}
		idI := StatID(repoID, ranked[i].Kind, ranked[i].Fingerprint)
		idJ := StatID(repoID, ranked[j].Kind, ranked[j].Fingerprint)
		return idI < idJ
	})
	if offset >= len(ranked) {
		return nil
	}
	ranked = ranked[offset:]
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

// AssemblePage assembles one store page of groups into findings, merging
// per-rule suppression counts across the page and sorting score desc,
// finding id asc. Groups that suppress below two survivors contribute
// their counts but no finding.
func AssemblePage(repoID string, kind Kind, groups []Group, includeTests bool) ([]Finding, map[string]int) {
	findings := make([]Finding, 0, len(groups))
	suppressions := map[string]int{}
	for _, group := range groups {
		finding, ok := AssembleFinding(repoID, kind, group.Fingerprint, group.Members, includeTests)
		for rule, count := range finding.Suppressions {
			suppressions[rule] += count
		}
		if !ok {
			continue
		}
		findings = append(findings, finding)
	}
	sortFindings(findings)
	return findings, suppressions
}

// sortFindings orders findings by score desc, then finding id asc: a
// deterministic page order with no hidden tiebreaks.
func sortFindings(findings []Finding) {
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Score != findings[j].Score {
			return findings[i].Score > findings[j].Score
		}
		return findings[i].ID < findings[j].ID
	})
}

// SortFindings orders assembled findings by final post-suppression score
// desc, finding id asc. The read surface emits merged cross-kind pages in
// this order: stat-score window order ranks pre-suppression groups for
// paging, but suppression changes scores unequally, so emission follows
// the final scores the client actually sees.
func SortFindings(findings []Finding) {
	sortFindings(findings)
}
