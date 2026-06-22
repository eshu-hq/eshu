package ruby

import "strings"

// rubyCallMatch is one call shape recovered from a single source line.
type rubyCallMatch struct {
	name     string
	fullName string
	args     string
}

// rubyParseCalls reconstructs the ordered set of call shapes from one trimmed
// source line. It applies six successive recognizers — chained, scoped,
// qualified, bare known-method, receiverless parenthesized, receiverless
// assignment, and receiverless statement — deduplicating by full name so each
// distinct call is emitted once per line in recognizer order.
func rubyParseCalls(line string) []rubyCallMatch {
	calls := make([]rubyCallMatch, 0)
	seen := make(map[string]struct{})

	rubyScanChainedCalls(line, &calls, seen)
	rubyScanScopedCalls(line, &calls, seen)
	rubyScanQualifiedCalls(line, &calls, seen)
	rubyScanBareCalls(line, &calls, seen)
	rubyScanReceiverlessParenCalls(line, &calls, seen)
	rubyScanReceiverlessAssignmentCalls(line, &calls, seen)
	rubyScanReceiverlessStatementCalls(line, &calls, seen)

	return calls
}

func rubyAppendCall(calls *[]rubyCallMatch, seen map[string]struct{}, fullName string, args string) {
	if fullName == "" {
		return
	}
	if _, ok := seen[fullName]; ok {
		return
	}
	seen[fullName] = struct{}{}
	*calls = append(*calls, rubyCallMatch{
		name:     rubyCallName(fullName),
		fullName: fullName,
		args:     args,
	})
}

// rubyScanChainedCalls recognizes `receiver.method(args).method2` shapes where a
// parenthesized call is chained into a further method, mirroring the legacy
// chained-call pattern.
func rubyScanChainedCalls(line string, calls *[]rubyCallMatch, seen map[string]struct{}) {
	for index := 0; index < len(line); index++ {
		if !rubyIsReceiverStart(line, index) {
			continue
		}
		receiverEnd, ok := rubyScanDottedReceiver(line, index)
		if !ok || receiverEnd >= len(line) || line[receiverEnd] != '(' {
			continue
		}
		argsEnd, _, ok := rubyScanParenArgs(line, receiverEnd)
		if !ok {
			continue
		}
		if argsEnd >= len(line) || line[argsEnd] != '.' {
			continue
		}
		methodStart := argsEnd + 1
		methodEnd := rubyScanMethodName(line, methodStart)
		if methodEnd == methodStart {
			continue
		}
		receiver := strings.TrimSpace(line[index:receiverEnd])
		methodName := strings.TrimSpace(line[methodStart:methodEnd])
		if receiver == "" || methodName == "" {
			continue
		}
		fullName := receiver + "." + methodName
		if _, ok := seen[fullName]; ok {
			continue
		}
		trailingArgs := rubyChainTrailingArgs(line, methodEnd)
		rubyAppendCall(calls, seen, fullName, trailingArgs)
		index = methodEnd - 1
	}
}

// rubyChainTrailingArgs captures the argument text following a chained method,
// either a parenthesized list or the remaining same-line tail up to a comment.
func rubyChainTrailingArgs(line string, methodEnd int) string {
	if methodEnd < len(line) {
		switch line[methodEnd] {
		case '(':
			_, args, ok := rubyScanParenArgs(line, methodEnd)
			if ok {
				return strings.TrimSpace(args)
			}
		case ' ', '\t':
			tail := rubyTrailingNonComment(line, methodEnd)
			if strings.TrimSpace(tail) != "" {
				return strings.TrimSpace(tail)
			}
		}
	}
	return ""
}

// rubyScanScopedCalls recognizes `Constant.method(` shapes where the receiver is
// a capitalized scoped constant directly invoking a parenthesized method.
func rubyScanScopedCalls(line string, calls *[]rubyCallMatch, seen map[string]struct{}) {
	for index := 0; index < len(line); index++ {
		if line[index] < 'A' || line[index] > 'Z' {
			continue
		}
		if index > 0 && rubyIsConstantBodyByte(line[index-1]) {
			continue
		}
		constEnd := rubyScanScopedConstant(line, index)
		if constEnd >= len(line) || line[constEnd] != '.' {
			continue
		}
		methodStart := constEnd + 1
		methodEnd := rubyScanCallMethodName(line, methodStart)
		if methodEnd == methodStart || methodEnd >= len(line) || line[methodEnd] != '(' {
			continue
		}
		fullName := strings.TrimSpace(line[index:methodEnd])
		fullName = rubyRestoreCallPunctuation(line, fullName)
		rubyAppendCall(calls, seen, fullName, "")
		index = methodEnd - 1
	}
}

// rubyScanQualifiedCalls recognizes `receiver.method` (and longer dotted chains)
// not necessarily followed by parentheses, mirroring the qualified-call pattern.
func rubyScanQualifiedCalls(line string, calls *[]rubyCallMatch, seen map[string]struct{}) {
	for index := 0; index < len(line); index++ {
		if !rubyIsReceiverStart(line, index) {
			continue
		}
		end, count := rubyScanQualifiedChain(line, index)
		if count == 0 {
			continue
		}
		boundary := end
		if boundary < len(line) {
			switch line[boundary] {
			case '?', '!', '=':
				boundary++
			}
		}
		if !rubyQualifiedBoundaryOK(line, boundary) {
			continue
		}
		fullName := strings.TrimSpace(line[index:boundary])
		fullName = rubyRestoreCallPunctuation(line, fullName)
		rubyAppendCall(calls, seen, fullName, "")
		index = boundary - 1
	}
}

// rubyScanBareCalls recognizes a fixed set of receiverless Ruby DSL and Kernel
// methods that take same-line arguments, mirroring the bare-call pattern.
func rubyScanBareCalls(line string, calls *[]rubyCallMatch, seen map[string]struct{}) {
	for index := 0; index < len(line); index++ {
		if index > 0 && rubyIsBarePrefixByte(line[index-1]) {
			continue
		}
		name, nameEnd := rubyMatchBareCallKeyword(line, index)
		if name == "" {
			continue
		}
		args, ok := rubyBareCallArgs(line, nameEnd)
		if !ok {
			continue
		}
		if _, seenName := seen[name]; seenName {
			index = nameEnd - 1
			continue
		}
		rubyAppendCall(calls, seen, name, args)
		index = nameEnd - 1
	}
}

// rubyBareCallArgs returns the argument text for a bare call: either a
// parenthesized list or a required same-line tail (up to a comment).
func rubyBareCallArgs(line string, nameEnd int) (string, bool) {
	if nameEnd < len(line) && line[nameEnd] == '(' {
		_, args, ok := rubyScanParenArgs(line, nameEnd)
		if ok {
			return strings.TrimSpace(args), true
		}
	}
	if nameEnd < len(line) && (line[nameEnd] == ' ' || line[nameEnd] == '\t') {
		tail := rubyTrailingNonComment(line, nameEnd)
		if strings.TrimSpace(tail) != "" {
			return strings.TrimSpace(tail), true
		}
	}
	return "", false
}

// rubyScanReceiverlessParenCalls recognizes `name(args)` lowercase receiverless
// calls that are not Ruby keywords.
func rubyScanReceiverlessParenCalls(line string, calls *[]rubyCallMatch, seen map[string]struct{}) {
	for index := 0; index < len(line); index++ {
		if index > 0 && rubyIsReceiverlessPrefixByte(line[index-1]) {
			continue
		}
		nameEnd := rubyScanLowerCallName(line, index)
		if nameEnd == index || nameEnd >= len(line) {
			continue
		}
		argsStart := nameEnd
		for argsStart < len(line) && (line[argsStart] == ' ' || line[argsStart] == '\t') {
			argsStart++
		}
		if argsStart >= len(line) || line[argsStart] != '(' {
			continue
		}
		_, args, ok := rubyScanParenArgs(line, argsStart)
		if !ok {
			continue
		}
		fullName := strings.TrimSpace(line[index:nameEnd])
		if fullName == "" || rubyIsIgnoredReceiverlessCall(fullName) {
			continue
		}
		rubyAppendCall(calls, seen, fullName, args)
		index = nameEnd - 1
	}
}

// rubyScanReceiverlessAssignmentCalls recognizes `= name` shapes where a bare
// lowercase method is the right-hand value, mirroring the assignment pattern.
func rubyScanReceiverlessAssignmentCalls(line string, calls *[]rubyCallMatch, seen map[string]struct{}) {
	for index := 0; index < len(line); index++ {
		if line[index] != '=' {
			continue
		}
		cursor := index + 1
		for cursor < len(line) && (line[cursor] == ' ' || line[cursor] == '\t') {
			cursor++
		}
		nameStart := cursor
		nameEnd := rubyScanLowerCallName(line, nameStart)
		if nameEnd == nameStart {
			continue
		}
		if !rubyReceiverlessAssignmentBoundary(line, nameEnd) {
			continue
		}
		fullName := strings.TrimSpace(line[nameStart:nameEnd])
		if fullName == "" || rubyIsIgnoredReceiverlessCall(fullName) {
			continue
		}
		rubyAppendCall(calls, seen, fullName, "")
	}
}

// rubyScanReceiverlessStatementCalls recognizes a leading `name rest` statement
// where a bare lowercase method heads the line with same-line arguments.
func rubyScanReceiverlessStatementCalls(line string, calls *[]rubyCallMatch, seen map[string]struct{}) {
	index := 0
	for index < len(line) && (line[index] == ' ' || line[index] == '\t') {
		index++
	}
	nameEnd := rubyScanLowerCallName(line, index)
	if nameEnd == index || nameEnd >= len(line) {
		return
	}
	if line[nameEnd] != ' ' && line[nameEnd] != '\t' {
		return
	}
	rest := line[nameEnd+1:]
	if strings.ContainsAny(rest, "#=") {
		return
	}
	rest = strings.TrimRight(rest, " \t")
	if rest == "" {
		return
	}
	for nameEnd > index && (line[nameEnd-1] == ' ' || line[nameEnd-1] == '\t') {
		nameEnd--
	}
	fullName := strings.TrimSpace(line[index:nameEnd])
	if fullName == "" || rubyIsIgnoredReceiverlessCall(fullName) {
		return
	}
	rubyAppendCall(calls, seen, fullName, strings.TrimSpace(rest))
}

func rubyIsIgnoredReceiverlessCall(name string) bool {
	switch name {
	case "and", "begin", "break", "case", "class", "def", "defined", "do", "else",
		"elsif", "end", "ensure", "false", "for", "if", "in", "module", "next",
		"nil", "not", "or", "redo", "rescue", "retry", "return", "self", "super",
		"then", "true", "undef", "unless", "until", "when", "while", "yield":
		return true
	default:
		return false
	}
}

func rubyCallName(fullName string) string {
	trimmed := strings.TrimSpace(fullName)
	if trimmed == "" {
		return ""
	}
	if index := strings.LastIndex(trimmed, "."); index >= 0 {
		return trimmed[index+1:]
	}
	if index := strings.LastIndex(trimmed, "::"); index >= 0 {
		return trimmed[index+2:]
	}
	return trimmed
}

// rubyRestoreCallPunctuation preserves Ruby predicate, bang, and writer method
// suffixes when the dotted-call recognizers matched only the bare name.
func rubyRestoreCallPunctuation(line string, fullName string) string {
	if strings.HasSuffix(fullName, "?") || strings.HasSuffix(fullName, "!") || strings.HasSuffix(fullName, "=") {
		return fullName
	}
	for _, suffix := range []string{"?", "!", "="} {
		if strings.Contains(line, fullName+suffix) {
			return fullName + suffix
		}
	}
	return fullName
}

func rubyInferAssignmentType(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "Unknown"
	}
	if index := strings.Index(trimmed, "="); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[index+1:])
	}
	trimmed = strings.TrimSpace(strings.TrimSuffix(trimmed, ";"))
	trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "new "))
	if trimmed == "" {
		return "Unknown"
	}
	return trimmed
}

func rubyParseArguments(raw string) []string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return []string{}
	}
	segments := strings.Split(trimmed, ",")
	args := make([]string, 0, len(segments))
	for _, segment := range segments {
		arg := rubyNormalizeArgument(segment)
		if arg == "" {
			continue
		}
		args = append(args, arg)
	}
	return args
}

func rubyNormalizeArgument(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	trimmed = strings.TrimPrefix(trimmed, "&")
	trimmed = strings.TrimPrefix(trimmed, "*")
	trimmed = strings.TrimPrefix(trimmed, ":")
	if index := strings.Index(trimmed, "="); index >= 0 {
		trimmed = strings.TrimSpace(trimmed[:index])
	}
	if index := strings.Index(trimmed, ":"); index >= 0 && !strings.Contains(trimmed, "://") {
		if strings.Count(trimmed, ":") == 1 {
			trimmed = strings.TrimSpace(trimmed[:index])
		}
	}
	if len(trimmed) >= 2 {
		if (trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'') || (trimmed[0] == '"' && trimmed[len(trimmed)-1] == '"') {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
	}
	return trimmed
}
