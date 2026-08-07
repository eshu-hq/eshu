# Scan visible Markdown lines for a caller-supplied anchored regular expression.
# The caller sets pattern and mode=count|line. HTML comments are replaced by
# opaque boundaries: visible prefixes and suffixes survive, but a suffix cannot
# be promoted into Markdown syntax at column zero. Fenced code does not
# contribute matches. Fence closing follows CommonMark character and length
# rules so a shorter or different marker cannot expose hidden content.

BEGIN {
	if (pattern == "" || (mode != "count" && mode != "line")) {
		print "context-stories-markdown-visible: invalid pattern or mode" > "/dev/stderr"
		exit 2
	}
	comment_boundary = sprintf("%c", 28)
}

function is_fence_run(text, closing,    indent, trimmed, char, run, tail) {
	indent = 0
	while (substr(text, indent + 1, 1) == " ") indent++
	if (indent > 3 || substr(text, indent + 1, 1) == "\t") return 0
	trimmed = substr(text, indent + 1)
	char = substr(trimmed, 1, 1)
	if (char != "`" && char != "~") return 0
	run = 0
	while (substr(trimmed, run + 1, 1) == char) run++
	if (run < 3) return 0
	tail = substr(trimmed, run + 1)
	if (closing &&
	    (char != fence_char || run < fence_length || tail !~ /^[ \t]*$/)) return 0
	detected_char = char
	detected_length = run
	return 1
}

function visible_with_comment_boundaries(text,    visible, remaining, marker) {
	visible = ""
	remaining = text
	while (remaining != "") {
		if (in_comment) {
			marker = index(remaining, "-->")
			if (!marker) return visible comment_boundary
			visible = visible comment_boundary
			remaining = substr(remaining, marker + 3)
			in_comment = 0
		} else {
			marker = index(remaining, "<!--")
			if (!marker) return visible remaining
			visible = visible substr(remaining, 1, marker - 1) comment_boundary
			remaining = substr(remaining, marker + 4)
			in_comment = 1
		}
	}
	return visible
}

{
	if (in_fence) {
		if (is_fence_run($0, 1)) in_fence = 0
		next
	}
	line = visible_with_comment_boundaries($0)
	if (is_fence_run(line, 0)) {
		in_fence = 1
		fence_char = detected_char
		fence_length = detected_length
		next
	}
	if (line ~ pattern) {
		if (mode == "line") {
			print NR
			found = 1
			exit
		}
		count++
	}
}

END {
	if (mode == "line") {
		if (!found) exit 1
	} else {
		print count + 0
	}
}
