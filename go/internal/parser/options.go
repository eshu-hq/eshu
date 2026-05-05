package parser

import "strings"

// Options configures one parser execution.
type Options struct {
	IndexSource   bool
	VariableScope string
}

func (o Options) normalizedVariableScope() string {
	scope := strings.TrimSpace(strings.ToLower(o.VariableScope))
	if scope == "" {
		return "module"
	}
	if scope == "all" {
		return "all"
	}
	return "module"
}
