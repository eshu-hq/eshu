// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package recordpseudo

import (
	"regexp"
	"strings"
)

// awsSpacedPhrases are AWS-authored phrases that lead a spaced ARN
// component and are followed only by an AWS-issued ID.
var awsSpacedPhrases = []string{"CloudFront Origin Access Identity "}

// learnSpacedComponent learns a resource component that contains spaces.
// An AWS phrase keeps its words and learns only the ID-shaped tokens after
// it. Anything else is a customer's free text (a CloudWatch alarm name,
// for example): every word of four or more characters is learned, so it is
// rewritten even when no other field names the resource.
func (d *dictionary) learnSpacedComponent(component string) {
	for _, phrase := range awsSpacedPhrases {
		if tail, ok := strings.CutPrefix(component, phrase); ok {
			for _, field := range strings.Fields(tail) {
				if len(field) >= minSubstituteLen && spacedIDTokenRe.MatchString(field) {
					d.learnIdent(field)
				}
			}
			return
		}
	}
	for _, field := range strings.Fields(component) {
		if len(field) >= minSubstituteLen {
			d.learnIdent(field)
		}
	}
}

// spacedIDTokenRe is an ID-shaped token inside a spaced ARN component: it
// mixes letters and digits, or it is an uppercase run of eight or more.
// AWS's fixed phrase words ("CloudFront Origin Access Identity") are
// title-case and digit-free, so they never match.
var spacedIDTokenRe = regexp.MustCompile(`^(?:[A-Za-z0-9]*[0-9][A-Za-z0-9]*[A-Za-z][A-Za-z0-9]*|[A-Za-z0-9]*[A-Za-z][A-Za-z0-9]*[0-9][A-Za-z0-9]*|[A-Z0-9]{8,})$`)
