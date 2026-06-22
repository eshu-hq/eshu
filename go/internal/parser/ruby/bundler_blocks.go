package ruby

import (
	"regexp"
	"strings"
)

// rubyOpaqueBlockPattern matches Ruby control and DSL block openers used by the
// bundler Gemfile scanner to balance `end` tokens against the surrounding
// group, source, or platform context.
var rubyOpaqueBlockPattern = regexp.MustCompile(`^(?:if|unless|case|begin|for|while|until)\b|\bdo\b`)

// rubyStartsOpaqueBlock reports whether a trimmed Gemfile line opens a control
// or DSL block whose matching `end` should not pop the surrounding bundler
// context. It is part of the bundler manifest parser, which intentionally keeps
// its own line-oriented recognizers separate from the AST source parser.
func rubyStartsOpaqueBlock(trimmed string) bool {
	if !rubyOpaqueBlockPattern.MatchString(trimmed) {
		return false
	}
	return !strings.Contains(trimmed, " end")
}
