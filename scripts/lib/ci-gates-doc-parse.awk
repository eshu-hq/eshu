# ci-gates-doc-parse.awk - parse specs/ci-gates.v1.yaml and render one
# markdown table row per gate. Read alongside scripts/generate-ci-gates-doc.sh,
# which emits the page header/table header via printf and then invokes this
# script for the table body.
#
# Parsing and rendering are combined in one awk pass deliberately: an earlier
# draft split them (awk parses to a TAB-delimited line, a bash `while read`
# loop renders each row) and hit a real bash 3.2.57 defect — the system bash
# this repo's scripts must run under (see the repo's PATH-order rule) does
# not reliably split `read` fields on a delimiter outside its three built-in
# IFS-whitespace characters (space/tab/newline): a TAB delimiter collapses
# adjacent empty fields (POSIX IFS-whitespace behavior), and a SOH (0x01)
# delimiter does not split at all on this bash, even though the byte is
# genuinely present in the input. Doing both parse and render in awk sidesteps
# the entire class of bug — no shell field-splitting of generated data ever
# happens.
#
# Three record shapes exist in the registry (validated against the live file
# before this parser was written):
#
#   1. A full local+CI gate: id, name, category, tier, blocking, a
#      local.command, a ci.workflow/ci.job pair, and a triggers list.
#   2. A CI-only gate (5 in the live registry: reducer-contention, e2e-tests,
#      trivy-image, docker-publish, macos-build): no local: block at all —
#      the command cell falls back to ci_only_reason (needs Postgres, Docker
#      Compose, GHCR credentials, a release token, or a macOS runner).
#   3. An alias entry (6 in the live registry, e.g. no-ai-attribution-message,
#      frontend-format-staged): only id and a reason string (a staged- or
#      commit-msg-stage variant of another gate's check, sharing its
#      command). Rendered as its own row shape rather than guessing values
#      that were never in the registry.
#
# Fail-closed: a record whose id is empty, or a file with zero records, is a
# parser or registry bug, not a silent empty table.

BEGIN {
	have_record = 0
	records_seen = 0
	section = ""
}

function reset_record() {
	id = ""; name = ""; category = ""; tier = ""; blocking = ""
	command = ""; workflow = ""; job = ""; reason = ""; ci_only_reason = ""
	trigger_count = 0
	delete triggers
	section = ""
}

# escape_pipe protects a markdown-table hazard: one committed command in the
# live registry contains a literal "|" inside a `-run 'Test(A|B|C)'` regex
# alternation, which would otherwise be read as a new table column.
function escape_pipe(s) {
	gsub(/\|/, "\\|", s)
	return s
}

function render_row() {
	if (id == "") {
		print "ci-gates-doc-parse: FATAL: a record flushed with an empty id" > "/dev/stderr"
		exit 1
	}

	if (category == "" && reason != "") {
		# Alias entry.
		name_cell = "*(alias — " escape_pipe(reason) ")*"
		category_cell = "—"; tier_cell = "—"; blocking_cell = "—"
		command_cell = "—"; ci_cell = "—"; triggers_cell = "—"
	} else {
		name_cell = escape_pipe(name)
		category_cell = (category != "" ? category : "—")
		tier_cell = (tier != "" ? tier : "—")
		blocking_cell = (blocking != "" ? blocking : "—")
		if (command != "") {
			command_cell = "`" escape_pipe(command) "`"
		} else if (ci_only_reason != "") {
			command_cell = "— (CI-only: " escape_pipe(ci_only_reason) ")"
		} else {
			command_cell = "—"
		}
		if (workflow != "") {
			ci_cell = workflow
			if (job != "") {
				ci_cell = ci_cell " / " escape_pipe(job)
			}
		} else {
			ci_cell = "—"
		}
		if (trigger_count > 0) {
			sample = ""
			shown = trigger_count
			if (shown > 3) {
				shown = 3
			}
			for (i = 1; i <= shown; i++) {
				sample = sample (i > 1 ? ", " : "") triggers[i]
			}
			if (trigger_count > shown) {
				sample = sample ", …"
			}
			triggers_cell = trigger_count " path(s): " escape_pipe(sample)
		} else {
			triggers_cell = "—"
		}
	}

	printf "| `%s` | %s | %s | %s | %s | %s | %s | %s |\n", \
		id, name_cell, category_cell, tier_cell, blocking_cell, \
		command_cell, ci_cell, triggers_cell
}

# A new gate record starts. Render whatever record was in progress first.
/^  - id: / {
	if (have_record) {
		render_row()
	}
	reset_record()
	have_record = 1
	records_seen++
	id = $0
	sub(/^  - id: /, "", id)
	next
}

# The non_gate_workflows section uses `  - file:` entries (not gate records) and
# each carries its own `reason:`. Without this rule those reason lines keep
# matching `/^    reason: / && have_record` and overwrite the reason of the LAST
# gate/alias record — which is still open because no new `  - id:` follows it —
# so that record renders with a non_gate_workflows reason (the prepr-stamp-verify
# / refresh-cassettes leak). Flush the pending record and stop field capture at
# the first `  - file:` so nothing past the gate/alias records bleeds in.
/^  - file: / {
	if (have_record) {
		render_row()
		have_record = 0
	}
	next
}

/^    name: / && have_record {
	name = $0; sub(/^    name: /, "", name); next
}
/^    category: / && have_record {
	category = $0; sub(/^    category: /, "", category); next
}
/^    tier: / && have_record {
	tier = $0; sub(/^    tier: /, "", tier); next
}
/^    blocking: / && have_record {
	blocking = $0; sub(/^    blocking: /, "", blocking); next
}
/^    reason: / && have_record {
	reason = $0; sub(/^    reason: /, "", reason); gsub(/^"|"$/, "", reason); next
}
/^    ci_only_reason: / && have_record {
	ci_only_reason = $0; sub(/^    ci_only_reason: /, "", ci_only_reason); gsub(/^"|"$/, "", ci_only_reason); next
}
/^    triggers:/ && have_record {
	section = "triggers"; next
}
/^    local:/ && have_record {
	section = "local"; next
}
/^    ci:/ && have_record {
	section = "ci"; next
}
/^    requirements:/ && have_record {
	section = "requirements"; next
}
/^    [a-z_]+:/ && have_record {
	# Any other 4-space-indented key (ci_only_reason, hook_id, …) closes
	# whichever list/section was open, so trigger accumulation cannot leak
	# past its own block.
	section = ""; next
}

section == "triggers" && /^      - "/ && have_record {
	v = $0; sub(/^      - "/, "", v); sub(/"$/, "", v)
	trigger_count++
	triggers[trigger_count] = v
	next
}
section == "local" && /^      command: / && have_record {
	command = $0; sub(/^      command: /, "", command); gsub(/^"|"$/, "", command); next
}
section == "ci" && /^      workflow: / && have_record {
	workflow = $0; sub(/^      workflow: /, "", workflow); gsub(/^"|"$/, "", workflow); next
}
section == "ci" && /^      job: / && have_record {
	job = $0; sub(/^      job: /, "", job); gsub(/^"|"$/, "", job); next
}

END {
	# A record is still open only when no `  - file:` (non_gate_workflows) block
	# followed the last gate/alias record; flush it. `records_seen` (never reset)
	# is the real empty-registry signal — have_record can legitimately be 0 here
	# because the `  - file:` rule flushes and closes the final record.
	if (have_record) {
		render_row()
	}
	if (records_seen == 0) {
		print "ci-gates-doc-parse: FATAL: no gate records found (empty or malformed registry)" > "/dev/stderr"
		exit 1
	}
}
