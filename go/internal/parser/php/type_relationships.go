package php

import "strings"

func parsePHPImplementedInterfaces(kind string, tail string) []string {
	if kind != "class" {
		return nil
	}
	remaining := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(tail), "{"))
	if remaining == "" {
		return nil
	}
	index := strings.Index(remaining, "implements")
	if index < 0 {
		return nil
	}
	return appendPHPBaseList(strings.TrimSpace(remaining[index+len("implements"):]))
}
