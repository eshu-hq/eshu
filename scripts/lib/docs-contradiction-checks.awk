# docs-contradiction-checks.awk - the two self-contradiction checks behind
# scripts/verify-docs-contradiction.sh (#5340). Invoked once with every
# non-generated docs/public/**/*.md path as an argument, run from inside
# docs/public so FILENAME is already the doc-relative path used in output.
#
# Check 1 (modal-polarity, shared-subject anchor): a doc self-contradicts when
# the SAME specific subject is described as both implemented/supported/available
# AND not-implemented/not-supported/not-yet-implemented/not-yet-supported/
# planned/unsupported. The anchor must be a shared,
# specific subject — a backticked code span, a bare capability-ID string
# (three-plus hyphen-joined components, e.g. slim-web-route-detection), or a
# stopword-free three-word n-gram — never bare co-occurrence of a positive and
# a negative word in the same file. That anchor requirement is the deliberate
# false-positive guard: a page that says "X is supported for A; Y is deferred
# for B" must not be flagged just because it contains both a positive and
# unrelated negative word (see php.md's Laravel capability row, which pairs
# "implemented"/"supported" with "deferred" — not a tracked negative phrase —
# for a DIFFERENT capability than the one graded "implemented").
#
# Check 2 (duplicate table-row key): within one contiguous markdown table
# block (reset at a blank line or any line that does not start with "|"),
# the same first-column cell value is not supposed to repeat. Two rows
# sharing a label inside one table are either a stale row nobody deleted or a
# copy-paste error; either way, the reader cannot tell which row is current
# truth.
#
# No word-boundary metacharacter (`\<`/`\>`) is used: this awk's regex engine
# (the same one-true-awk build that ships in /usr/bin/awk on macOS and mawk on
# the ubuntu-latest GHA runner) has no GNU boundary extension. Instead, every
# line is normalized to a space-padded, lowercased, non-alnum-collapsed-to-
# single-space string before matching, so a plain " word " substring check is
# an exact word match without relying on any awk beyond POSIX.

BEGIN {
	split("implemented supported available planned unsupported is are now not yet " \
		"a an the of to in on at and or for with that this these those it its as by " \
		"from into than then when where which who whom whose will would can could " \
		"should must may might do does did have has had but if so such be been was " \
		"were no all also only still both either neither more most other some any " \
		"each every per", stop_list, " ")
	for (i in stop_list) {
		stopword[stop_list[i]] = 1
	}
	prev_filename = ""
}

# strip_link_urls drops the URL portion of a markdown link
# ([text](https://...) -> [text]) before any other processing. Left
# unstripped, a table of per-ecosystem issue-tracker links (the common case
# in status-matrix reference pages) turns "github.com/org/repo/issues/1050"
# into anchor noise that repeats across nearly every row, since every row
# cites the same handful of tracking issues.
function strip_link_urls(line,    out) {
	out = line
	gsub(/\]\([^)]*\)/, "]", out)
	return out
}

# norm lowercases a line, collapses every run of non-alphanumeric characters
# to a single space, and pads both ends with a space so " word " matches an
# exact token without a word-boundary regex extension.
function norm(line,    n) {
	n = " " tolower(line) " "
	gsub(/[^a-z0-9]+/, " ", n)
	gsub(/ +/, " ", n)
	return n
}

function is_positive(n) {
	if (n ~ / is implemented /) return 1
	if (n ~ / are implemented /) return 1
	if (n ~ / now implemented /) return 1
	# The bare " supported " check must NOT fire on " not supported " /
	# " not yet supported ": those are negative statements, and classifying
	# them positive would make "X is not supported" read as both polarities
	# (or, on a supported-vs-not-supported page, hide the contradiction as a
	# false green). Exclude them here so such a line is negative-only.
	if (n ~ / supported / && n !~ / not supported / && n !~ / not yet supported /) return 1
	if (n ~ / is available /) return 1
	return 0
}

function is_negative(n) {
	if (n ~ / not implemented /) return 1
	if (n ~ / not yet implemented /) return 1
	if (n ~ / not supported /) return 1
	if (n ~ / not yet supported /) return 1
	if (n ~ / planned /) return 1
	if (n ~ / unsupported /) return 1
	return 0
}

function slugify(s,    out) {
	out = tolower(s)
	gsub(/[^a-z0-9]+/, "-", out)
	gsub(/^-+|-+$/, "", out)
	if (length(out) > 60) {
		out = substr(out, 1, 60)
	}
	return out
}

# record_anchor appends a 1-indexed occurrence line number to a space-list
# keyed by the anchor's exact text, in the given map (by array-name prefix so
# the two anchor kinds — exact spans and n-grams — stay in separate maps). A
# same-line, same-key repeat is a no-op: a backticked capability ID is found
# by BOTH extract_backticks and extract_capability_ids on the same line, and
# double-recording that single real occurrence would inflate
# max_anchor_occurrences()'s count without adding a real distinct location.
function record_anchor(map_name, key, line,    already, cur) {
	if (map_name == "exact") {
		cur = exact_lines[key]
	} else {
		cur = ngram_lines[key]
	}
	already = (cur == " " line || cur ~ (" " line "$"))
	if (already) {
		return
	}
	if (map_name == "exact") {
		exact_lines[key] = cur " " line
	} else {
		ngram_lines[key] = cur " " line
	}
}

# extract_backticks records every inline `code span` on the line as an exact
# anchor candidate. Spans under 2 or over 100 chars are skipped: too short to
# be a specific subject, too long to be a citation rather than a paragraph
# accidentally wrapped in backticks.
function extract_backticks(line,    s, start, tail, endpos, span) {
	s = line
	while ((start = index(s, "`")) > 0) {
		tail = substr(s, start + 1)
		endpos = index(tail, "`")
		if (endpos == 0) {
			break
		}
		span = substr(tail, 1, endpos - 1)
		# A bare single alphabetic word in backticks (`supported`,
		# `unsupported`, `true`, `exact`) is a status ENUM VALUE — the
		# polarity signal itself, written as code for table-cell styling —
		# not a specific subject. It recurs across unrelated rows of a
		# status-matrix table, so treating it as an anchor produces
		# combinatorial noise rather than a real contradiction. A span with
		# a hyphen, digit, punctuation, or space is still eligible: those
		# shapes name an actual identifier, path, or call, e.g.
		# `slim-web-route-detection` or `$app->group()`.
		if (length(span) >= 2 && length(span) <= 100 && span !~ /^[A-Za-z]+$/) {
			record_anchor("exact", span, FNR)
		}
		s = substr(tail, endpos + 1)
	}
}

# extract_capability_ids records every bare (non-backticked as far as this
# check cares — a backticked one is also caught by extract_backticks, and a
# duplicate anchor key just collapses harmlessly) hyphen-joined identifier of
# three or more components, e.g. slim-web-route-detection.
function extract_capability_ids(line,    s, m) {
	s = tolower(line)
	while (match(s, /[a-z][a-z0-9]*(-[a-z0-9]+){2,}/)) {
		m = substr(s, RSTART, RLENGTH)
		record_anchor("exact", m, FNR)
		s = substr(s, RSTART + RLENGTH)
	}
}

# extract_ngrams records every stopword-free, contiguous three-word window of
# the normalized line as an n-gram anchor candidate. Excluding the polarity
# trigger words themselves (implemented, supported, not, yet, ...) is what
# keeps the anchor pinned to the SUBJECT of the sentence rather than the verb
# phrase that makes the file's co-occurrence check trivially true.
function extract_ngrams(n,    toks, tcount, i, phrase) {
	tcount = split(n, toks, " ")
	for (i = 1; i <= tcount - 2; i++) {
		if (toks[i] == "" || toks[i+1] == "" || toks[i+2] == "") continue
		if (stopword[toks[i]] || stopword[toks[i+1]] || stopword[toks[i+2]]) continue
		# A minimum 3-character content word and an all-digits exclusion both
		# fight the same noise source: issue numbers, short URL path
		# fragments, and other structural tokens that repeat across every row
		# of a status-matrix table without naming a shared capability.
		if (length(toks[i]) < 3 || length(toks[i+1]) < 3 || length(toks[i+2]) < 3) continue
		if (toks[i] ~ /^[0-9]+$/ || toks[i+1] ~ /^[0-9]+$/ || toks[i+2] ~ /^[0-9]+$/) continue
		phrase = toks[i] " " toks[i+1] " " toks[i+2]
		record_anchor("ngram", phrase, FNR)
	}
}

# MAX_ANCHOR_OCCURRENCES bounds how common an anchor may be (total lines it
# appears on, positive or negative) before it is too generic — boilerplate
# shared across nearly every row of a status-matrix table, not a specific
# subject — to trust as a contradiction anchor.
function max_anchor_occurrences() {
	return 6
}

# emit_if_contradiction checks one anchor's occurrence-line list for a
# genuine contradiction and, if found, prints the finding. Two guards beyond
# "has a positive line and a negative line" matter here:
#   - first_pos != first_neg: a single line matching both regexes is a
#     compound sentence about two different things (or, as with
#     `unsupported_metadata_source`, an enum-value identifier that merely
#     contains a trigger substring after punctuation normalization) — not
#     the same subject asserted twice with opposite polarity.
#   - occurrence count <= max_anchor_occurrences(): an anchor repeated across
#     many rows/lines is boilerplate (a shared issue link, a repeated
#     ecosystem name), not a specific claim.
function emit_if_contradiction(relpath, key, list,    lcount, ln, i, has_pos, has_neg, first_pos, first_neg, lns) {
	lcount = split(list, lns, " ")
	if (lcount > max_anchor_occurrences()) {
		return
	}
	has_pos = 0; has_neg = 0; first_pos = 0; first_neg = 0
	for (i = 1; i <= lcount; i++) {
		ln = lns[i] + 0
		if (pos[ln] && !has_pos) { has_pos = 1; first_pos = ln }
		if (neg[ln] && !has_neg) { has_neg = 1; first_neg = ln }
	}
	if (has_pos && has_neg && first_pos != first_neg) {
		printf "%s polarity:%s pos=%d neg=%d anchor=\"%s\"\n", \
			relpath, slugify(key), first_pos, first_neg, key
	}
}

# finalize_polarity walks both anchor maps for the file just finished and
# emits one finding per anchor that survives emit_if_contradiction's guards.
# Output shape:
#   "<relpath> polarity:<slug> pos=<line> neg=<line> anchor=\"<text>\""
# Only the first two space-separated fields are the baseline key; the rest is
# human-readable detail for a gate-failure log line.
function finalize_polarity(relpath,    key) {
	for (key in exact_lines) {
		emit_if_contradiction(relpath, key, exact_lines[key])
	}
	for (key in ngram_lines) {
		emit_if_contradiction(relpath, key, ngram_lines[key])
	}
}

# table_reset clears the duplicate-row tracking for the block just closed.
function table_reset() {
	delete row_seen
	delete row_first_line
	in_table = 0
}

# process_table_line feeds one raw line into the duplicate-row-key check. A
# line's own leading/trailing whitespace is trimmed before checking for the
# "|" table-row marker; a separator row (only -, :, |, and whitespace) is
# skipped since it carries no data.
function process_table_line(line,    trimmed, cells, ncells, first, sep_only, i, slug) {
	trimmed = line
	gsub(/^[ \t]+|[ \t]+$/, "", trimmed)
	if (trimmed !~ /^\|/) {
		table_reset()
		return
	}
	if (!in_table) {
		table_reset()
		in_table = 1
	}
	ncells = split(trimmed, cells, "|")
	sep_only = 1
	for (i = 1; i <= ncells; i++) {
		if (cells[i] !~ /^[ \t]*:?-+:?[ \t]*$/ && cells[i] !~ /^[ \t]*$/) {
			sep_only = 0
		}
	}
	if (sep_only) {
		return
	}
	first = (ncells >= 2 ? cells[2] : cells[1])
	gsub(/^[ \t]+|[ \t]+$/, "", first)
	if (first == "") {
		return
	}
	slug = slugify(first)
	if (row_seen[slug]) {
		printf "%s duplicate-row:%s first_line=%d dup_line=%d key=\"%s\"\n", \
			pending_relpath, slug, row_first_line[slug], FNR, first
	} else {
		row_seen[slug] = 1
		row_first_line[slug] = FNR
	}
}

function reset_file_state() {
	delete pos
	delete neg
	delete exact_lines
	delete ngram_lines
	table_reset()
	in_code = 0
}

FNR == 1 {
	if (prev_filename != "") {
		finalize_polarity(prev_filename)
	}
	reset_file_state()
	prev_filename = FILENAME
	pending_relpath = FILENAME
}

/^```/ {
	in_code = !in_code
	table_reset()
	next
}

in_code {
	table_reset()
	next
}

{
	stripped = strip_link_urls($0)
	n = norm(stripped)
	pos[FNR] = is_positive(n)
	neg[FNR] = is_negative(n)
	extract_backticks(stripped)
	extract_capability_ids(stripped)
	extract_ngrams(n)
	process_table_line($0)
}

END {
	if (prev_filename != "") {
		finalize_polarity(prev_filename)
	}
}
