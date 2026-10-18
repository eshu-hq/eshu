// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

const (
	// graphWriteRetryReasonParserIntervalRace labels a retry of a false
	// "Variable `X` not defined" SyntaxError that the Neo4j query parser
	// reports because of a data race in its bundled ANTLR runtime (#8190).
	graphWriteRetryReasonParserIntervalRace = "neo4j_parser_interval_race"

	// antlrIntervalCacheMaxOffset is the largest character index ANTLR 4.13.2
	// Interval.of caches. The cache is unsynchronized (antlr/antlr4#4901), so
	// only a one-character token at or below this index can be corrupted.
	antlrIntervalCacheMaxOffset = 1000

	// parserRaceLogNameLimit bounds the reported-name field of the WARN log.
	// The corrupted name can be a prefix of the whole statement text.
	parserRaceLogNameLimit = 32
)

// neo4jUndefinedVariablePattern matches the header of Neo4j's undefined
// variable error. The name group is multi-line safe because the race can report
// a prefix of the statement text as the name.
var neo4jUndefinedVariablePattern = regexp.MustCompile(
	"(?s)^Variable `(.*?)` not defined \\(line ([0-9]+), column ([0-9]+) \\(offset: ([0-9]+)\\)\\)")

// neo4jParserIntervalRace describes a recognized parser race failure.
type neo4jParserIntervalRace struct {
	statement Statement
	offset    int
	name      string
}

// detectNeo4jParserIntervalRace reports whether err is the false undefined
// variable error produced by the ANTLR Interval.of cache race in the Neo4j
// parser (antlr/antlr4#4901), as opposed to a real undefined variable.
//
// This is a vendor-bug workaround. Remove it once the pinned Neo4j image
// bundles an ANTLR runtime that contains antlr/antlr4#4902 (the 4.13.2 runtime
// in Neo4j 2026.08.1 does not).
//
// The race corrupts the text of a one-character token at character index
// <= 1000: a reader sees a half-built cached Interval and reads either
// character 0 of the query or a prefix of it. Neo4j then reports that text, at
// the correct position of the token, as the undefined variable. A match
// therefore needs all of:
//   - a Neo4jError with code Statement.SyntaxError and the exact undefined
//     variable header;
//   - one statement whose character at the reported offset is a one-character
//     identifier at a token boundary, at an offset the cache covers, with the
//     reported line and column agreeing with the offset;
//   - the reported name either a prefix of that statement's text (the corrupted
//     cache read) or equal to the identifier (the defining-site read).
//
// Statement shape is deliberately not checked. The error is raised while the
// statement is parsed, before it runs, and every group RetryingExecutor
// receives runs as one managed transaction (one session.ExecuteWrite), so the
// driver rolls the whole transaction back and nothing was applied. Replay
// safety rests on that atomicity, not on AllStatementsAreReplaySafe; the
// non-first-generation canonical group that hit #8190 holds
// `generation_id <> $generation_id` retracts that the shape check rejects. The
// class stays bound to this exact header and code, never to a commit-time
// error shape.
//
// A real undefined one-character variable is indistinguishable from the
// defining-site read. It costs at most MaxRetries retries (WARN-logged and
// counted) and then surfaces as itself: runWithRetry returns the original
// SyntaxError, terminal, once the budget is spent. The retry of a real race
// succeeds because each cache slot is published once per JVM.
func detectNeo4jParserIntervalRace(err error, stmts []Statement) (neo4jParserIntervalRace, bool) {
	var none neo4jParserIntervalRace
	if err == nil || len(stmts) == 0 {
		return none, false
	}
	var neo4jErr *neo4jdriver.Neo4jError
	if !errors.As(err, &neo4jErr) || neo4jErr.Code != nornicDBStatementSyntaxErrorCode {
		return none, false
	}
	m := neo4jUndefinedVariablePattern.FindStringSubmatch(neo4jErr.Msg)
	if m == nil {
		return none, false
	}
	line, lineErr := strconv.Atoi(m[2])
	col, colErr := strconv.Atoi(m[3])
	offset, offsetErr := strconv.Atoi(m[4])
	if lineErr != nil || colErr != nil || offsetErr != nil || offset > antlrIntervalCacheMaxOffset {
		return none, false
	}
	for _, stmt := range stmts {
		if parserRaceMatchesStatement(stmt.Cypher, m[1], line, col, offset) {
			return neo4jParserIntervalRace{statement: stmt, offset: offset, name: m[1]}, true
		}
	}
	return none, false
}

// parserRaceMatchesStatement applies the per-statement checks of
// detectNeo4jParserIntervalRace. Offsets index code points; a statement with
// non-ASCII text before the offset that Neo4j indexes differently fails closed.
func parserRaceMatchesStatement(cypher, name string, line, col, offset int) bool {
	runes := []rune(cypher)
	if offset >= len(runes) || !isCypherIdentStart(runes[offset]) {
		return false
	}
	if offset > 0 && isCypherIdentPart(runes[offset-1]) {
		return false
	}
	if offset+1 < len(runes) && isCypherIdentPart(runes[offset+1]) {
		return false
	}
	wantLine, wantCol := 1, offset+1
	for i, r := range runes[:offset] {
		if r == '\n' {
			wantLine++
			wantCol = offset - i
		}
	}
	if line != wantLine || col != wantCol {
		return false
	}
	return name == string(runes[offset]) || strings.HasPrefix(cypher, name)
}

func isCypherIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isCypherIdentPart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// classifyNeo4jParserIntervalRace returns the retry reason for a recognized
// parser race failure, or "".
func classifyNeo4jParserIntervalRace(err error, stmts []Statement) string {
	if _, ok := detectNeo4jParserIntervalRace(err, stmts); ok {
		return graphWriteRetryReasonParserIntervalRace
	}
	return ""
}

// logNeo4jParserIntervalRaceRetry emits the operator WARN for a retried parser
// race failure: write phase, statement summary, offset, and a length-bounded
// reported name. It never logs row parameters. The reported name is the
// corrupted token text, which can be a prefix of the fixed statement template,
// so up to parserRaceLogNameLimit characters of that template can appear.
func logNeo4jParserIntervalRaceRetry(
	ctx context.Context,
	operationLabel string,
	attempt int,
	err error,
	stmts []Statement,
) {
	race, ok := detectNeo4jParserIntervalRace(err, stmts)
	if !ok {
		return
	}
	name := race.name
	if runes := []rune(name); len(runes) > parserRaceLogNameLimit {
		name = string(runes[:parserRaceLogNameLimit])
	}
	attrs := []any{
		"reason", graphWriteRetryReasonParserIntervalRace,
		"write_phase", operationLabel,
		"attempt", attempt,
		"offset", race.offset,
		"reported_name", name,
		"reported_name_length", len([]rune(race.name)),
	}
	if summary, ok := race.statement.Parameters[StatementMetadataSummaryKey].(string); ok && summary != "" {
		attrs = append(attrs, "statement_summary", summary)
	}
	slog.WarnContext(ctx, "neo4j parser interval race (antlr4#4901) reported a false undefined variable, retrying", attrs...)
}
