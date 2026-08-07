# Scan visible Markdown lines for a caller-supplied anchored regular expression.
# The caller sets pattern and mode=count|line. HTML comments and fenced code do
# not contribute matches; fence closing follows CommonMark character and length
# rules so a shorter or different marker cannot expose hidden content.

BEGIN {
	if (pattern == "" || (mode != "count" && mode != "line")) {
		print "context-stories-markdown-visible: invalid pattern or mode" > "/dev/stderr"
		exit 2
	}
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

{
	line = $0
	if (in_fence) {
		if (is_fence_run(line, 1)) in_fence = 0
		next
	}
	if (in_comment) {
		if (index(line, "-->")) in_comment = 0
		next
	}
	if (index(line, "<!--")) {
		after_start = substr(line, index(line, "<!--") + 4)
		if (!index(after_start, "-->")) in_comment = 1
		next
	}
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
