package golang

import (
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

type goAccessPathOptions struct {
	maxParts  int
	truncated *int
}

func (o goAccessPathOptions) normalizedMaxParts() int {
	if o.maxParts <= 0 {
		return goDefaultAccessPathParts
	}
	return o.maxParts
}

func appendBaseAccessPath(uses []string, node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) []string {
	operand := node.ChildByFieldName("operand")
	if operand == nil {
		return uses
	}
	base, ok := goAccessPathWithOptions(operand, source, aliases, options)
	if !ok || base == "" || base == blankIdentifier {
		return uses
	}
	return appendUnique(uses, base)
}

func goAccessPathWithOptions(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) (string, bool) {
	parts, ok := goAccessPathParts(node, source, aliases, options)
	if !ok || len(parts) == 0 {
		return "", ok
	}
	maxParts := options.normalizedMaxParts()
	if len(parts) > maxParts {
		if options.truncated != nil {
			*options.truncated = *options.truncated + 1
		}
		truncated := append([]string{}, parts[:maxParts]...)
		truncated = append(truncated, "*")
		return strings.Join(truncated, "."), true
	}
	return strings.Join(parts, "."), true
}

func goAccessPathParts(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) ([]string, bool) {
	if node == nil {
		return nil, false
	}
	switch node.Kind() {
	case "identifier":
		resolved := aliases.resolve(nodeText(node, source))
		if resolved == "" {
			return nil, true
		}
		return strings.Split(resolved, "."), true
	case "selector_expression":
		base, ok := goAccessPathParts(node.ChildByFieldName("operand"), source, aliases, options)
		if !ok || len(base) == 0 {
			return nil, false
		}
		field := nodeText(node.ChildByFieldName("field"), source)
		if field == "" {
			return nil, false
		}
		return append(base, field), true
	case "index_expression":
		base, ok := goAccessPathParts(node.ChildByFieldName("operand"), source, aliases, options)
		if !ok || len(base) == 0 {
			return nil, false
		}
		indexed := append([]string{}, base...)
		indexed[len(indexed)-1] += "[*]"
		return indexed, true
	case "parenthesized_expression":
		return goAccessPathParts(firstNamedChild(node), source, aliases, options)
	}
	return nil, false
}

func goAssignTargetPathWithOptions(node *tree_sitter.Node, source []byte, aliases goBindingAliases, options goAccessPathOptions) (string, bool) {
	if node == nil {
		return "", false
	}
	if node.Kind() == "identifier" {
		return nodeText(node, source), true
	}
	return goAccessPathWithOptions(node, source, aliases, options)
}

type goBindingAliases map[string]string

func (a goBindingAliases) resolve(name string) string {
	if name == "" {
		return ""
	}
	seen := map[string]struct{}{}
	for {
		next, ok := a[name]
		if !ok || next == "" {
			return name
		}
		if _, cycle := seen[name]; cycle {
			return name
		}
		seen[name] = struct{}{}
		name = next
	}
}

func (a goBindingAliases) clone() goBindingAliases {
	if len(a) == 0 {
		return goBindingAliases{}
	}
	out := make(goBindingAliases, len(a))
	for k, v := range a {
		out[k] = v
	}
	return out
}

func (a goBindingAliases) applyAssignment(node *tree_sitter.Node, source []byte) {
	left := node.ChildByFieldName("left")
	right := node.ChildByFieldName("right")
	if left == nil {
		return
	}
	targets := goAssignTargetsWithAliases(left, source, nil)
	for _, target := range targets {
		delete(a, target)
	}
	if len(targets) != 1 || right == nil {
		return
	}
	if aliased, ok := goAliasTarget(right, source, a); ok {
		a[targets[0]] = aliased
	}
}

func goMergeAliases(a, b goBindingAliases) goBindingAliases {
	out := goBindingAliases{}
	for k, av := range a {
		if bv, ok := b[k]; ok && bv == av {
			out[k] = av
		}
	}
	return out
}

func goAliasTarget(node *tree_sitter.Node, source []byte, aliases goBindingAliases) (string, bool) {
	text := strings.TrimSpace(nodeText(node, source))
	if !strings.HasPrefix(text, "&") {
		if target, ok := aliases[text]; ok && target != "" {
			return aliases.resolve(target), true
		}
		return "", false
	}
	target := strings.TrimSpace(strings.TrimPrefix(text, "&"))
	if !goIdentifierLike(target) {
		return "", false
	}
	return aliases.resolve(target), true
}

func goIdentifierLike(value string) bool {
	if value == "" {
		return false
	}
	for i, r := range value {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return false
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
