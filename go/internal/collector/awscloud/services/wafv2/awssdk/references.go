// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awswafv2types "github.com/aws/aws-sdk-go-v2/service/wafv2/types"

	wafv2service "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/wafv2"
)

// ruleReferences collects only the reference identity a web ACL exposes: the
// ARNs of customer rule groups, IP sets, and regex pattern sets it points at,
// plus the vendor and name of managed rule groups. It never carries any match
// statement body (ByteMatch, Regex, Sqli, Xss, Size, Geo, LabelMatch, or any
// search string), because those reveal threat-detection hypotheses.
type ruleReferences struct {
	ruleGroupARNs []string
	ipSetARNs     []string
	regexSetARNs  []string
	managedRefs   []wafv2service.ManagedRuleSetRef
}

// walkRuleReferences traverses the top-level rules of a web ACL and extracts
// reference identity. Nested logical statements (And/Or/Not) and rate-based
// scope-down statements are followed so references buried inside boolean logic
// are still found, but no match-statement payload is read.
func walkRuleReferences(rules []awswafv2types.Rule) ruleReferences {
	refs := &referenceCollector{}
	for _, rule := range rules {
		refs.walk(rule.Statement)
	}
	return ruleReferences{
		ruleGroupARNs: refs.ruleGroupARNs.values(),
		ipSetARNs:     refs.ipSetARNs.values(),
		regexSetARNs:  refs.regexSetARNs.values(),
		managedRefs:   refs.managedRefs,
	}
}

type referenceCollector struct {
	ruleGroupARNs orderedSet
	ipSetARNs     orderedSet
	regexSetARNs  orderedSet
	managedRefs   []wafv2service.ManagedRuleSetRef
	managedSeen   map[string]struct{}
}

// walk inspects only the reference-bearing and logical statement fields. Every
// other Statement field (ByteMatch, Regex, Sqli, Xss, Size, Geo, LabelMatch,
// Asn) is intentionally never read, so its body cannot leak into facts.
func (r *referenceCollector) walk(statement *awswafv2types.Statement) {
	if statement == nil {
		return
	}
	if ref := statement.RuleGroupReferenceStatement; ref != nil {
		r.ruleGroupARNs.add(aws.ToString(ref.ARN))
	}
	if ref := statement.IPSetReferenceStatement; ref != nil {
		r.ipSetARNs.add(aws.ToString(ref.ARN))
	}
	if ref := statement.RegexPatternSetReferenceStatement; ref != nil {
		r.regexSetARNs.add(aws.ToString(ref.ARN))
	}
	if managed := statement.ManagedRuleGroupStatement; managed != nil {
		r.addManaged(managed)
		// A managed rule group may carry a scope-down statement that itself
		// references customer sets. Follow it for references only.
		r.walk(managed.ScopeDownStatement)
	}
	if and := statement.AndStatement; and != nil {
		for i := range and.Statements {
			r.walk(&and.Statements[i])
		}
	}
	if or := statement.OrStatement; or != nil {
		for i := range or.Statements {
			r.walk(&or.Statements[i])
		}
	}
	if not := statement.NotStatement; not != nil {
		r.walk(not.Statement)
	}
	if rate := statement.RateBasedStatement; rate != nil {
		r.walk(rate.ScopeDownStatement)
	}
}

func (r *referenceCollector) addManaged(managed *awswafv2types.ManagedRuleGroupStatement) {
	vendor := strings.TrimSpace(aws.ToString(managed.VendorName))
	name := strings.TrimSpace(aws.ToString(managed.Name))
	if vendor == "" && name == "" {
		return
	}
	key := vendor + "\x00" + name + "\x00" + strings.TrimSpace(aws.ToString(managed.Version))
	if r.managedSeen == nil {
		r.managedSeen = make(map[string]struct{})
	}
	if _, ok := r.managedSeen[key]; ok {
		return
	}
	r.managedSeen[key] = struct{}{}
	r.managedRefs = append(r.managedRefs, wafv2service.ManagedRuleSetRef{
		VendorName: vendor,
		Name:       name,
		Version:    strings.TrimSpace(aws.ToString(managed.Version)),
	})
}

// orderedSet collects distinct non-empty strings in insertion order so emitted
// reference lists are deterministic across repeated observations.
type orderedSet struct {
	seen    map[string]struct{}
	ordered []string
}

func (s *orderedSet) add(value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	if _, ok := s.seen[value]; ok {
		return
	}
	s.seen[value] = struct{}{}
	s.ordered = append(s.ordered, value)
}

func (s *orderedSet) values() []string {
	return s.ordered
}
