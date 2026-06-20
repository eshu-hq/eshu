package golang

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"strings"
	"unicode"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func goGuardText(node *tree_sitter.Node, source []byte) string {
	text := redactGoGuardLiterals(strings.TrimSpace(nodeText(node, source)))
	return strings.Join(strings.Fields(text), " ")
}

func goNegatedGuardText(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if unwrapped, ok := goWholeNegationOperand(trimmed); ok {
		return unwrapped
	}
	return "!(" + trimmed + ")"
}

func goWholeNegationOperand(text string) (string, bool) {
	expr, err := goparser.ParseExpr(normalizeGuardTextForParse(text))
	if err != nil {
		return "", false
	}
	unary, ok := expr.(*ast.UnaryExpr)
	if !ok || unary.Op != token.NOT {
		return "", false
	}
	operand := strings.TrimSpace(goExprText(text, unary.X))
	return trimBalancedOuterParens(operand), true
}

func normalizeGuardTextForParse(text string) string {
	return strings.ReplaceAll(text, "<literal>", "literal__")
}

func goExprText(text string, node ast.Node) string {
	start := int(node.Pos()) - 1
	end := int(node.End()) - 1
	if start < 0 || start > len(text) || end < start || end > len(text) {
		return text
	}
	return text[start:end]
}

func trimBalancedOuterParens(text string) string {
	trimmed := strings.TrimSpace(text)
	for strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")") && parensWrapWholeExpr(trimmed) {
		trimmed = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
	}
	return trimmed
}

func parensWrapWholeExpr(text string) bool {
	depth := 0
	for index := 0; index < len(text); index++ {
		switch text[index] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && index != len(text)-1 {
				return false
			}
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func redactGoGuardLiterals(text string) string {
	var out strings.Builder
	for index := 0; index < len(text); {
		ch := rune(text[index])
		switch {
		case text[index] == '"' || text[index] == '\'' || text[index] == '`':
			out.WriteString("<literal>")
			index = skipQuotedLiteral(text, index)
		case unicode.IsDigit(ch) && !isGoIdentByte(previousByte(text, index)):
			out.WriteString("<literal>")
			index = skipNumberLiteral(text, index)
		default:
			out.WriteByte(text[index])
			index++
		}
	}
	return out.String()
}

func skipQuotedLiteral(text string, index int) int {
	quote := text[index]
	index++
	for index < len(text) {
		if text[index] == '\\' && quote != '`' {
			index += 2
			continue
		}
		if text[index] == quote {
			return index + 1
		}
		index++
	}
	return index
}

func skipNumberLiteral(text string, index int) int {
	for index < len(text) {
		ch := text[index]
		if (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') || ch == '_' || ch == '.' {
			index++
			continue
		}
		break
	}
	return index
}

func previousByte(text string, index int) byte {
	if index == 0 {
		return 0
	}
	return text[index-1]
}

func isGoIdentByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
