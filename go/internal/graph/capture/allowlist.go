// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package capture

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/backendconformance"
)

// upstreamIssueRE pins the allowlist's upstream contract: the link must
// point at a real issue on a GitHub repository, so every excused divergence
// stays tracked against a fixable item rather than a wiki page or a bare
// repo root.
var upstreamIssueRE = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+/issues/[0-9]+$`)

// AllowlistEntry is one accepted backend divergence: the normalized
// statement text it excuses (plus optional bound parameters), the divergence
// kind it excuses (see the tier vocabulary below), the reason it is
// accepted, the upstream issue tracking it, and its owner.
type AllowlistEntry struct {
	Statement  string `yaml:"statement"`
	Parameters string `yaml:"parameters"`
	Tier       string `yaml:"tier"`
	Reason     string `yaml:"reason"`
	Upstream   string `yaml:"upstream"`
	Owner      string `yaml:"owner"`
}

// The tier vocabulary scopes an entry to one divergence kind, so an excuse
// for scheduling noise can never cover a result disagreement on the same
// statement. "statement" excuses any kind on the named statement (a genuine
// whole-statement dialect divergence); any other tier excuses only its named
// kind (see backendconformance.Divergence*). There is no executions or
// rowcount tier: execution-count and row-total noise with agreeing results
// is advisory at the gate (backendconformance.AdvisoryKind, #6782 permanent
// disposition as extended by the option-2 slice), so neither needs an
// excuse, and every remaining tier must match at least one divergence per
// run or the entry is stale.
var allowlistTiers = map[string]string{
	"statement": "",
	"missing":   backendconformance.DivergenceMissing,
	"results":   backendconformance.DivergenceResults,
	"failures":  backendconformance.DivergenceFailures,
}

// Allowlist is the parsed divergence allowlist. It is empty by default:
// every divergence fails the gate until an entry names it with a reason
// and an upstream issue link.
type Allowlist struct {
	entries   []AllowlistEntry
	transient []TransientRead
	tieOrder  []TieOrderRead
}

// TransientRead is one registered transient-state read (option 1,
// #6782): an orphan scan or readiness poll whose result depends on where
// the drain is when the page is read, so a digest disagreement across
// legs is timing, not backend truth. Unlike an allowlist entry it takes
// no tier — the exclusion covers the observed-noise kinds by design —
// and it is never stale-checked, because a transient read agrees on most
// runs by definition.
type TransientRead struct {
	Statement  string `yaml:"statement"`
	Parameters string `yaml:"parameters"`
	Reason     string `yaml:"reason"`
	Upstream   string `yaml:"upstream"`
	Owner      string `yaml:"owner"`
}

// transientReadMarkers are the syntactic proof a statement reads
// transient state: uid-anchored orphan scans and the orphan-observation
// timestamp the sweep pages on. The parse guard requires one of them, so
// a steady-state read can never be registered as transient by accident.
var transientReadMarkers = []string{"uid IS NULL", "eshu_orphan_observed_at_unix"}

// TieOrderRead is one registered tie-order read (#6782 entry-44 flap):
// an ORDER BY read with no truncation whose keys can tie, so delivery
// order is backend-undefined while the row multiset always agrees. Like
// a transient read it takes no tier and is never stale-checked — tied
// delivery agrees on most runs by definition — but the parse guard
// requires ORDER BY and rejects LIMIT/SKIP: with truncation, tied keys
// change which rows return, and that is row truth, not order-only noise.
type TieOrderRead struct {
	Statement  string `yaml:"statement"`
	Parameters string `yaml:"parameters"`
	Reason     string `yaml:"reason"`
	Upstream   string `yaml:"upstream"`
	Owner      string `yaml:"owner"`
}

// ParseAllowlist parses and validates raw allowlist YAML. A missing reason
// or a missing/non-issue upstream link fails the parse, so a divergence
// can never be silenced without an explanation and a tracked issue.
func ParseAllowlist(raw []byte) (*Allowlist, error) {
	var document struct {
		Entries   []AllowlistEntry `yaml:"entries"`
		Transient []yaml.Node      `yaml:"transient_reads"`
		TieOrder  []yaml.Node      `yaml:"tie_order_reads"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("parse divergence allowlist: %w", err)
	}
	transient, err := parseTransientReads(document.Transient)
	if err != nil {
		return nil, err
	}
	tieOrder, err := parseTieOrderReads(document.TieOrder)
	if err != nil {
		return nil, err
	}
	for i, entry := range document.Entries {
		if strings.TrimSpace(entry.Statement) == "" {
			return nil, fmt.Errorf("divergence allowlist entry %d: statement is required", i)
		}
		if _, ok := allowlistTiers[strings.TrimSpace(entry.Tier)]; !ok {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): tier must be one of statement, missing, results, failures (executions and rowcount are advisory at the gate since #6782 and need no entry)", i, entry.Statement)
		}
		if strings.TrimSpace(entry.Reason) == "" {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): reason is required", i, entry.Statement)
		}
		if !upstreamIssueRE.MatchString(strings.TrimSpace(entry.Upstream)) {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): upstream must be a GitHub issue URL", i, entry.Statement)
		}
	}
	return &Allowlist{entries: document.Entries, transient: transient, tieOrder: tieOrder}, nil
}

// parseTransientReads validates the transient_reads section: same
// accountability as an allowlist entry (statement, reason, upstream
// issue), no tier key (strict-decoded, so a tier is a parse error rather
// than a silent no-op), and the transient-state guard — the statement
// must carry one of the transientReadMarkers, proving it reads transient
// state rather than steady-state truth.
func parseTransientReads(nodes []yaml.Node) ([]TransientRead, error) {
	reads := make([]TransientRead, 0, len(nodes))
	for i, node := range nodes {
		var read TransientRead
		dec, err := strictNodeDecoder(node)
		if err != nil {
			return nil, fmt.Errorf("transient read %d: %w", i, err)
		}
		if err := dec.Decode(&read); err != nil {
			return nil, fmt.Errorf("transient read %d: %w", i, err)
		}
		if strings.TrimSpace(read.Statement) == "" {
			return nil, fmt.Errorf("transient read %d: statement is required", i)
		}
		if !isTransientStatement(read.Statement) {
			return nil, fmt.Errorf("transient read %d (%q): statement shows no transient-state marker (want one of %q)", i, read.Statement, transientReadMarkers)
		}
		if strings.TrimSpace(read.Reason) == "" {
			return nil, fmt.Errorf("transient read %d (%q): reason is required", i, read.Statement)
		}
		if !upstreamIssueRE.MatchString(strings.TrimSpace(read.Upstream)) {
			return nil, fmt.Errorf("transient read %d (%q): upstream must be a GitHub issue URL", i, read.Statement)
		}
		reads = append(reads, read)
	}
	return reads, nil
}

// parseTieOrderReads validates the tie_order_reads section: same
// accountability as a transient read (statement, reason, upstream
// issue), no tier key (strict-decoded), and the order-only guard — the
// statement must ORDER BY with no LIMIT or SKIP, proving only delivery
// order (never the row multiset) can differ across backends.
func parseTieOrderReads(nodes []yaml.Node) ([]TieOrderRead, error) {
	reads := make([]TieOrderRead, 0, len(nodes))
	for i, node := range nodes {
		var read TieOrderRead
		dec, err := strictNodeDecoder(node)
		if err != nil {
			return nil, fmt.Errorf("tie-order read %d: %w", i, err)
		}
		if err := dec.Decode(&read); err != nil {
			return nil, fmt.Errorf("tie-order read %d: %w", i, err)
		}
		if strings.TrimSpace(read.Statement) == "" {
			return nil, fmt.Errorf("tie-order read %d: statement is required", i)
		}
		if !isTieOrderStatement(read.Statement) {
			return nil, fmt.Errorf("tie-order read %d (%q): statement must ORDER BY with no LIMIT or SKIP (truncation makes tied keys row truth, not order-only noise)", i, read.Statement)
		}
		if strings.TrimSpace(read.Reason) == "" {
			return nil, fmt.Errorf("tie-order read %d (%q): reason is required", i, read.Statement)
		}
		if !upstreamIssueRE.MatchString(strings.TrimSpace(read.Upstream)) {
			return nil, fmt.Errorf("tie-order read %d (%q): upstream must be a GitHub issue URL", i, read.Statement)
		}
		reads = append(reads, read)
	}
	return reads, nil
}

// isTieOrderStatement reports whether the statement is order-only:
// an ORDER BY with no truncation. The ORDER BY leg reuses
// backendconformance.HasOrderBy — the same case-insensitive,
// literal-aware predicate the comparator uses to pick the digest — so
// the guard and the digest can never disagree about whether a read is
// ordered. Truncation is a whole-word, case-insensitive scan that skips
// string literals, backtick identifiers, and comments, for the same
// reason: Cypher keywords are case-insensitive, so a lowercase limit
// truncates exactly like an uppercase one.
func isTieOrderStatement(statement string) bool {
	return backendconformance.HasOrderBy(statement) && !hasTruncationClause(statement)
}

// hasTruncationClause reports whether cypher carries a top-level LIMIT
// or SKIP clause: whole words, case-insensitive, outside string
// literals, backtick-quoted identifiers, and line/block comments, so
// prose about limits cannot block a registration and a hidden clause
// cannot slip one through. Both quote styles honor the backslash escape
// and the doubled-quote escape.
func hasTruncationClause(cypher string) bool {
	var words []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			words = append(words, strings.ToUpper(current.String()))
			current.Reset()
		}
	}
	// skipQuoted consumes a quoted span starting at i (the opening quote
	// or backtick) and returns the index just past its closer.
	skipQuoted := func(i int, quote byte) int {
		for j := i + 1; j < len(cypher); j++ {
			switch cypher[j] {
			case '\\':
				j++
			case quote:
				if j+1 < len(cypher) && cypher[j+1] == quote {
					j++
					continue
				}
				return j + 1
			}
		}
		return len(cypher)
	}
	isWordByte := func(c byte) bool {
		return c == '_' || c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
	}
	for i := 0; i < len(cypher); {
		c := cypher[i]
		switch {
		case c == '/' && i+1 < len(cypher) && cypher[i+1] == '/':
			flush()
			next := strings.IndexByte(cypher[i:], '\n')
			if next < 0 {
				i = len(cypher)
			} else {
				i += next
			}
		case c == '/' && i+1 < len(cypher) && cypher[i+1] == '*':
			flush()
			end := strings.Index(cypher[i+2:], "*/")
			if end < 0 {
				i = len(cypher)
			} else {
				i += end + 4
			}
		case c == '\'' || c == '"' || c == '`':
			flush()
			i = skipQuoted(i, c)
		case isWordByte(c):
			current.WriteByte(c)
			i++
		default:
			flush()
			i++
		}
	}
	flush()
	for _, w := range words {
		if w == "LIMIT" || w == "SKIP" {
			return true
		}
	}
	return false
}

// strictNodeDecoder decodes one YAML mapping node with unknown fields
// rejected, so a misspelled or meaningless key (like a tier on a
// transient read) fails loudly instead of parsing into a zero value the
// gate would silently honor.
func strictNodeDecoder(node yaml.Node) (*yaml.Decoder, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	if err := enc.Encode(&node); err != nil {
		return nil, fmt.Errorf("re-encode entry: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close entry encoder: %w", err)
	}
	dec := yaml.NewDecoder(&buf)
	dec.KnownFields(true)
	return dec, nil
}

// isTransientStatement reports whether the statement carries a
// transient-state marker. Case-sensitive and fail-closed: a future read
// spelled differently is rejected at parse with the marker list, never
// admitted silently.
func isTransientStatement(statement string) bool {
	for _, marker := range transientReadMarkers {
		if strings.Contains(statement, marker) {
			return true
		}
	}
	return false
}

// ExcludeTransient removes the divergences registered transient reads
// explain and returns the rest alongside the excluded set for visible
// reporting. A transient entry matches by statement fingerprint with
// optional parameter narrowing, like an allowlist entry, but only for
// the observed-noise kinds (results, rowcount, executions): a backend
// error or a one-sided recording on a transient read stays required, so
// the exclusion can never mask a real breakage. Unlike Excuse there is
// no staleness error — a transient read agrees on most runs by design.
func (a *Allowlist) ExcludeTransient(diffs []backendconformance.DifferentialDifference) (remaining, excluded []backendconformance.DifferentialDifference) {
	if a == nil {
		return diffs, nil
	}
	for _, diff := range diffs {
		if transientExcludes(a.transient, diff) {
			excluded = append(excluded, diff)
			continue
		}
		remaining = append(remaining, diff)
	}
	return remaining, excluded
}

// transientExcludes reports whether any registered transient read covers
// diff's statement without narrowing it away by parameters, restricted
// to the observed-noise divergence kinds.
func transientExcludes(reads []TransientRead, diff backendconformance.DifferentialDifference) bool {
	switch diff.Kind {
	case backendconformance.DivergenceResults,
		backendconformance.DivergenceRowCount,
		backendconformance.DivergenceExecutions:
	default:
		return false
	}
	for _, read := range reads {
		if read.Statement != diff.Fingerprint.Statement {
			continue
		}
		if read.Parameters != "" && read.Parameters != diff.Fingerprint.Parameters {
			continue
		}
		return true
	}
	return false
}

// ExcludeTieOrder removes the divergences registered tie-order reads
// explain and returns the rest alongside the excluded set for visible
// reporting. A tie-order entry matches by statement fingerprint with
// optional parameter narrowing, like an allowlist entry, but only for
// the results kind: a backend error, a one-sided recording, or a count
// disagreement on a tie-order read stays required, so the exclusion can
// never mask a real breakage. Unlike Excuse there is no staleness
// error — tied delivery order agrees on most runs by design.
func (a *Allowlist) ExcludeTieOrder(diffs []backendconformance.DifferentialDifference) (remaining, excluded []backendconformance.DifferentialDifference) {
	if a == nil {
		return diffs, nil
	}
	for _, diff := range diffs {
		if tieOrderExcludes(a.tieOrder, diff) {
			excluded = append(excluded, diff)
			continue
		}
		remaining = append(remaining, diff)
	}
	return remaining, excluded
}

// tieOrderExcludes reports whether any registered tie-order read covers
// diff's statement without narrowing it away by parameters, restricted
// to the results divergence kind.
func tieOrderExcludes(reads []TieOrderRead, diff backendconformance.DifferentialDifference) bool {
	if diff.Kind != backendconformance.DivergenceResults {
		return false
	}
	for _, read := range reads {
		if read.Statement != diff.Fingerprint.Statement {
			continue
		}
		if read.Parameters != "" && read.Parameters != diff.Fingerprint.Parameters {
			continue
		}
		return true
	}
	return false
}

// Excuse removes the divergences the allowlist names and returns the rest.
// An allowlist entry must match at least one divergence in the run:
// Validate reports entries that matched nothing, so a fixed bug cannot
// linger as a permanent exemption.
func (a *Allowlist) Excuse(diffs []backendconformance.DifferentialDifference) ([]backendconformance.DifferentialDifference, error) {
	if a == nil {
		return diffs, nil
	}
	matched := make([]bool, len(a.entries))
	var remaining []backendconformance.DifferentialDifference
	for _, diff := range diffs {
		excused := false
		for i, entry := range a.entries {
			if entryMatches(entry, diff) {
				matched[i] = true
				excused = true
				break
			}
		}
		if !excused {
			remaining = append(remaining, diff)
		}
	}
	for i, entry := range a.entries {
		if !matched[i] {
			return nil, fmt.Errorf("divergence allowlist entry %d (%q): matched no divergence in this run (stale)", i, entry.Statement)
		}
	}
	return remaining, nil
}

// Validate reports whether every allowlist entry matched at least one of
// the run's divergences, without excusing anything. It is the check the
// gate runs even when the diff itself is clean, so a stale entry fails on
// a green run too.
func (a *Allowlist) Validate(diffs []backendconformance.DifferentialDifference) error {
	_, err := a.Excuse(diffs)
	return err
}

// entryMatches reports whether entry excuses diff. Parameters narrow the
// match when set; an empty parameters field excuses the statement under
// any bindings. The tier scopes the match to one divergence kind, except
// the statement tier, which excuses any kind on the named statement.
func entryMatches(entry AllowlistEntry, diff backendconformance.DifferentialDifference) bool {
	if entry.Statement != diff.Fingerprint.Statement {
		return false
	}
	kind, ok := allowlistTiers[strings.TrimSpace(entry.Tier)]
	if !ok {
		return false
	}
	if kind != "" && kind != diff.Kind {
		return false
	}
	return entry.Parameters == "" || entry.Parameters == diff.Fingerprint.Parameters
}
