package ruby

import (
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// recordCall composes the dotted call name for a (call) node from the AST and
// appends a deduplicated call row to the syntax view. Both the outer call and,
// for chained receivers, the inner receiver call are recorded because the walk
// descends into receivers separately; recording here keeps the full_name set in
// agreement with the legacy chained-call recognizer.
func (s *rubySyntax) recordCall(node *tree_sitter.Node, scopeStack []rubyScope) {
	method := node.ChildByFieldName("method")
	if method == nil {
		return
	}
	methodName := s.callMethodName(method)
	if methodName == "" {
		return
	}
	fullName := methodName
	if receiver := node.ChildByFieldName("receiver"); receiver != nil {
		receiverName := s.receiverName(receiver)
		if receiverName == "" {
			return
		}
		fullName = receiverName + "." + methodName
	} else if rubyIsIgnoredReceiverlessCall(methodName) {
		return
	}
	s.appendCall(node, scopeStack, fullName)
}

// recordAssignmentCall records a bare lowercase identifier used as the right
// value of an assignment as a receiverless call, matching the legacy
// assignment recognizer (e.g. `x = build_scopes` emits a `build_scopes` call).
func (s *rubySyntax) recordAssignmentCall(node *tree_sitter.Node, scopeStack []rubyScope) {
	right := node.ChildByFieldName("right")
	if right == nil || right.Kind() != "identifier" {
		return
	}
	name := s.text(right)
	if name == "" || rubyIsIgnoredReceiverlessCall(name) {
		return
	}
	if name[0] < 'a' || name[0] > 'z' {
		if name[0] != '_' {
			return
		}
	}
	s.appendCall(right, scopeStack, name)
}

// appendCall builds one function-call row from a node carrying the call's line
// and the resolved dotted full name, deduplicating by full name plus line.
func (s *rubySyntax) appendCall(node *tree_sitter.Node, scopeStack []rubyScope, fullName string) {
	line := shared.NodeLine(node)
	key := fullName + ":" + strconv.Itoa(line)
	if _, ok := s.seenCalls[key]; ok {
		return
	}
	s.seenCalls[key] = struct{}{}
	item := map[string]any{
		"name":              rubyCallName(fullName),
		"full_name":         fullName,
		"line_number":       line,
		"args":              []string{},
		"inferred_obj_type": nil,
		"lang":              "ruby",
		"is_dependency":     false,
	}
	contextName, contextType := rubyEnclosingContext(scopeStack, rubyScopeClass, rubyScopeModule, rubyScopeDef)
	if contextName != "" {
		item["context"] = contextName
		item["context_type"] = string(contextType)
		if contextType == rubyScopeClass {
			item["class_context"] = contextName
		}
	}
	if className := rubyEnclosingClassName(scopeStack); className != "" {
		item["class_context"] = className
	}
	s.calls = append(s.calls, item)
}

// callMethodName returns the textual method name of a (call) method field. The
// node is an identifier or constant whose text already carries any predicate,
// bang, or writer suffix.
func (s *rubySyntax) callMethodName(node *tree_sitter.Node) string {
	switch node.Kind() {
	case "identifier", "constant":
		return s.text(node)
	default:
		return shared.LastPathSegment(s.text(node), "::")
	}
}

// receiverName composes the dotted name of a call receiver. A call receiver
// recurses to its own dotted name; other receiver kinds use their last `::`
// segment-preserving text without argument lists.
func (s *rubySyntax) receiverName(node *tree_sitter.Node) string {
	switch node.Kind() {
	case "call":
		method := node.ChildByFieldName("method")
		if method == nil {
			return ""
		}
		methodName := s.callMethodName(method)
		if methodName == "" {
			return ""
		}
		if receiver := node.ChildByFieldName("receiver"); receiver != nil {
			receiverName := s.receiverName(receiver)
			if receiverName == "" {
				return ""
			}
			return receiverName + "." + methodName
		}
		return methodName
	case "scope_resolution", "constant":
		return s.text(node)
	case "identifier", "instance_variable", "self":
		return s.text(node)
	default:
		return ""
	}
}

// rubyIsIgnoredReceiverlessCall reports whether a receiverless identifier is a
// Ruby keyword that must not be treated as a method call.
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

// rubyCallName returns the trailing segment of a dotted or scoped call name.
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

// rubyInferAssignmentType returns the right-hand type label of an assignment
// expression text, stripping a `new ` prefix and trailing terminators.
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

// rubyNormalizeArgument strips block, splat, symbol, default-value, keyword, and
// quote decoration from one raw argument token.
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
