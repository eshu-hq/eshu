package python

import (
	"regexp"
	"strings"
)

// pythonDefNameRe captures the function name from a def or async def statement
// at the start of a (trimmed) line. It anchors to the line start so it only
// matches a real definition, never a def token appearing mid-expression.
var pythonDefNameRe = regexp.MustCompile(`^(?:async\s+)?def\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(`)

// submatchByIndex extracts capture group n from a FindAllStringSubmatchIndex
// location slice, returning "" when the group did not participate in the match.
func submatchByIndex(source string, loc []int, n int) string {
	lo := loc[2*n]
	hi := loc[2*n+1]
	if lo < 0 || hi < 0 {
		return ""
	}
	return source[lo:hi]
}

// pythonRouteHandlerForDecorator returns the handler function name bound to a
// route decorator located at byte offset decoratorStart in source. Starting at
// the decorator line, it advances to the next non-decorator, non-blank line and
// returns that line's def name. Stacked decorators between the route decorator
// and the def (e.g. @app.get("/x") / @auth_required / def handler) are skipped.
//
// The handler is bound ONLY when the first non-decorator line is a def or async
// def. If that line is anything else (an assignment, another statement, end of
// file), the route stays unbound and "" is returned, so a handler is never
// guessed or mis-associated with the wrong route (correlation-truth, #2788).
func pythonRouteHandlerForDecorator(source string, decoratorStart int) string {
	if decoratorStart < 0 || decoratorStart >= len(source) {
		return ""
	}
	// Skip the remainder of the decorator's own line.
	rest := source[decoratorStart:]
	if newline := strings.IndexByte(rest, '\n'); newline >= 0 {
		rest = rest[newline+1:]
	} else {
		return ""
	}

	for _, rawLine := range strings.Split(rest, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		// Stacked decorators sit between the route decorator and the def; skip
		// them and keep scanning for the function definition.
		if strings.HasPrefix(line, "@") {
			continue
		}
		if match := pythonDefNameRe.FindStringSubmatch(line); len(match) == 2 {
			return match[1]
		}
		// The first non-decorator line is not a def: there is no unambiguous
		// handler for this route, so leave it unbound.
		return ""
	}
	return ""
}
