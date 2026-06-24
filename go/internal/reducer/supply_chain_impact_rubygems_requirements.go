// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strconv"
	"strings"
)

func rubyGemsRequirementContains(raw string, observed string) (bool, bool) {
	tokens := strings.Fields(strings.ReplaceAll(strings.TrimSpace(raw), ",", " "))
	if len(tokens) == 0 {
		return false, true
	}
	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if token == "~>" {
			i++
			if i >= len(tokens) {
				return false, false
			}
			token += tokens[i]
		}
		contains, ok := rubyGemsRequirementTokenContains(token, observed)
		if !ok || !contains {
			return false, ok
		}
	}
	return true, true
}

func rubyGemsRequirementTokenContains(token string, observed string) (bool, bool) {
	token = strings.TrimSpace(token)
	if strings.HasPrefix(token, "~>") {
		return rubyGemsPessimisticContains(strings.TrimSpace(strings.TrimPrefix(token, "~>")), observed)
	}
	return comparatorConstraintContains(token, observed, compareRubyGemsVersion)
}

func rubyGemsPessimisticContains(version string, observed string) (bool, bool) {
	if !validRubyGemsVersion(version) {
		return false, false
	}
	if ok, valid := comparatorConstraintContains(">="+version, observed, compareRubyGemsVersion); !valid || !ok {
		return false, valid
	}
	upper, ok := rubyGemsPessimisticUpperBound(version)
	if !ok {
		return false, false
	}
	return comparatorConstraintContains("<"+upper, observed, compareRubyGemsVersion)
}

func rubyGemsPessimisticUpperBound(version string) (string, bool) {
	core := strings.SplitN(strings.TrimSpace(version), "-", 2)[0]
	parts := strings.Split(core, ".")
	numeric := make([]int, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			return "", false
		}
		value, err := strconv.Atoi(part)
		if err != nil {
			break
		}
		numeric = append(numeric, value)
	}
	if len(numeric) == 0 {
		return "", false
	}
	index := 0
	if len(numeric) > 2 {
		index = len(numeric) - 2
	}
	numeric[index]++
	for i := index + 1; i < len(numeric); i++ {
		numeric[i] = 0
	}
	out := make([]string, len(numeric))
	for i, part := range numeric {
		out[i] = strconv.Itoa(part)
	}
	return strings.Join(out, "."), true
}
