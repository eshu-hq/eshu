// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	ec2InternetExposureStateExposed    = "exposed"
	ec2InternetExposureStateNotExposed = "not_exposed"
	ec2InternetExposureStateUnknown    = "unknown"

	ec2InternetExposureReasonPublicIPReachableFromInternetSG = "public_ip_reachable_from_internet_sg"
	ec2InternetExposureReasonNoPublicIP                      = "no_public_ip"
	ec2InternetExposureReasonPublicIPUnknown                 = "public_ip_unknown"
	ec2InternetExposureReasonENIAttachmentUnresolved         = "eni_attachment_unresolved"
	ec2InternetExposureReasonSecurityGroupAttachmentMissing  = "security_group_attachment_unresolved"
	ec2InternetExposureReasonReachabilityUnresolved          = "reachability_unresolved"
	ec2InternetExposureReasonNoInternetReachableSG           = "no_internet_reachable_sg"

	ec2InternetExposureSkipMissingIdentity = "missing_identity"
	ec2InternetExposureSkipTombstone       = "tombstone"

	ec2RelNetworkInterfaceAttachedToResource = "ec2_network_interface_attached_to_resource"
	ec2RelNetworkInterfaceUsesSecurityGroup  = "ec2_network_interface_uses_security_group"
)

type ec2InternetExposureTally struct {
	decisions       map[string]int
	decisionReasons map[ec2InternetExposureDecisionKey]int
	reasons         map[string]int
	skipped         map[string]int
}

func newEC2InternetExposureTally() ec2InternetExposureTally {
	return ec2InternetExposureTally{
		decisions:       make(map[string]int),
		decisionReasons: make(map[ec2InternetExposureDecisionKey]int),
		reasons:         make(map[string]int),
		skipped:         make(map[string]int),
	}
}

type ec2InternetExposureDecisionKey struct {
	outcome string
	reason  string
}

func (t ec2InternetExposureTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

type ec2InternetExposureDecision struct {
	state           string
	internetExposed any
	reason          string
}

// ExtractEC2InternetExposureRows derives conservative EC2 internet-exposure
// node-property rows from EC2 posture, ENI relationship, and security-group rule
// facts. It never persists raw public IP addresses and never turns missing ENI,
// SG, or rule evidence into a safe false.
func ExtractEC2InternetExposureRows(
	postureEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
	ruleEnvelopes []facts.Envelope,
) ([]map[string]any, ec2InternetExposureTally) {
	tally := newEC2InternetExposureTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally
	}

	relationships := buildEC2InternetExposureRelationshipIndex(relationshipEnvelopes)
	rules := buildEC2InternetExposureRuleIndex(ruleEnvelopes)
	byUID := make(map[string]map[string]any, len(postureEnvelopes))
	for _, env := range sortedEC2InternetExposurePostures(postureEnvelopes) {
		if env.IsTombstone {
			tally.skipped[ec2InternetExposureSkipTombstone]++
			continue
		}
		uid, instanceID, ok := ec2InternetExposureIdentity(env)
		if !ok {
			tally.skipped[ec2InternetExposureSkipMissingIdentity]++
			continue
		}
		decision := deriveEC2InternetExposureDecision(env.Payload, instanceID, relationships, rules)
		tally.decisions[decision.state]++
		tally.decisionReasons[ec2InternetExposureDecisionKey{
			outcome: decision.state,
			reason:  decision.reason,
		}]++
		tally.reasons[decision.reason]++
		byUID[uid] = map[string]any{
			"uid":              uid,
			"state":            decision.state,
			"internet_exposed": decision.internetExposed,
			"reason":           decision.reason,
			"source_fact_id":   env.FactID,
		}
	}
	if len(byUID) == 0 {
		return nil, tally
	}
	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	rows := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		rows = append(rows, byUID[uid])
	}
	return rows, tally
}

func sortedEC2InternetExposurePostures(envelopes []facts.Envelope) []facts.Envelope {
	postures := make([]facts.Envelope, 0, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind != facts.EC2InstancePostureFactKind {
			continue
		}
		postures = append(postures, env)
	}
	sort.SliceStable(postures, func(i, j int) bool {
		left := payloadString(postures[i].Payload, "instance_id")
		right := payloadString(postures[j].Payload, "instance_id")
		if left != right {
			return left < right
		}
		return postures[i].FactID < postures[j].FactID
	})
	return postures
}

func ec2InternetExposureIdentity(env facts.Envelope) (uid, instanceID string, ok bool) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	instanceID = payloadString(env.Payload, "instance_id")
	arn := payloadString(env.Payload, "arn")
	resourceID := instanceID
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return "", "", false
	}
	resourceType := payloadString(env.Payload, "resource_type")
	if resourceType == "" {
		resourceType = "aws_ec2_instance"
	}
	return cloudResourceUID(accountID, region, resourceType, resourceID), resourceID, true
}

func deriveEC2InternetExposureDecision(
	payload map[string]any,
	instanceID string,
	relationships ec2InternetExposureRelationshipIndex,
	rules ec2InternetExposureRuleIndex,
) ec2InternetExposureDecision {
	publicIP := payloadBoolPointer(payload, "public_ip_associated")
	if publicIP == nil {
		return ec2InternetExposureDecision{
			state:           ec2InternetExposureStateUnknown,
			internetExposed: nil,
			reason:          ec2InternetExposureReasonPublicIPUnknown,
		}
	}
	if !*publicIP {
		return ec2InternetExposureDecision{
			state:           ec2InternetExposureStateNotExposed,
			internetExposed: false,
			reason:          ec2InternetExposureReasonNoPublicIP,
		}
	}

	enis := relationships.enisByInstance[instanceID]
	if len(enis) == 0 {
		return ec2InternetExposureDecision{
			state:           ec2InternetExposureStateUnknown,
			internetExposed: nil,
			reason:          ec2InternetExposureReasonENIAttachmentUnresolved,
		}
	}

	observedRule := false
	for eniID := range enis {
		sgs := relationships.sgsByENI[eniID]
		if len(sgs) == 0 {
			continue
		}
		for sgID := range sgs {
			if rules.internetIngressSGs[sgID] {
				return ec2InternetExposureDecision{
					state:           ec2InternetExposureStateExposed,
					internetExposed: true,
					reason:          ec2InternetExposureReasonPublicIPReachableFromInternetSG,
				}
			}
			if rules.observedIngressSGs[sgID] {
				observedRule = true
			}
		}
	}
	if !relationships.hasAnySGForENIs(enis) {
		return ec2InternetExposureDecision{
			state:           ec2InternetExposureStateUnknown,
			internetExposed: nil,
			reason:          ec2InternetExposureReasonSecurityGroupAttachmentMissing,
		}
	}
	if !observedRule {
		return ec2InternetExposureDecision{
			state:           ec2InternetExposureStateUnknown,
			internetExposed: nil,
			reason:          ec2InternetExposureReasonReachabilityUnresolved,
		}
	}
	return ec2InternetExposureDecision{
		state:           ec2InternetExposureStateNotExposed,
		internetExposed: false,
		reason:          ec2InternetExposureReasonNoInternetReachableSG,
	}
}

type ec2InternetExposureRelationshipIndex struct {
	enisByInstance map[string]map[string]struct{}
	sgsByENI       map[string]map[string]struct{}
}

func buildEC2InternetExposureRelationshipIndex(envelopes []facts.Envelope) ec2InternetExposureRelationshipIndex {
	index := ec2InternetExposureRelationshipIndex{
		enisByInstance: make(map[string]map[string]struct{}),
		sgsByENI:       make(map[string]map[string]struct{}),
	}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSRelationshipFactKind || env.IsTombstone {
			continue
		}
		relType := payloadString(env.Payload, "relationship_type")
		sourceID := payloadString(env.Payload, "source_resource_id")
		targetID := payloadString(env.Payload, "target_resource_id")
		targetType := payloadString(env.Payload, "target_type")
		if sourceID == "" || targetID == "" {
			continue
		}
		switch relType {
		case ec2RelNetworkInterfaceAttachedToResource:
			if targetType != "" && targetType != "aws_ec2_instance" {
				continue
			}
			addToSet(index.enisByInstance, targetID, sourceID)
		case ec2RelNetworkInterfaceUsesSecurityGroup:
			if targetType != "" && targetType != "aws_ec2_security_group" {
				continue
			}
			addToSet(index.sgsByENI, sourceID, targetID)
		}
	}
	return index
}

func (i ec2InternetExposureRelationshipIndex) hasAnySGForENIs(enis map[string]struct{}) bool {
	for eniID := range enis {
		if len(i.sgsByENI[eniID]) > 0 {
			return true
		}
	}
	return false
}

type ec2InternetExposureRuleIndex struct {
	internetIngressSGs map[string]bool
	observedIngressSGs map[string]bool
}

func buildEC2InternetExposureRuleIndex(envelopes []facts.Envelope) ec2InternetExposureRuleIndex {
	index := ec2InternetExposureRuleIndex{
		internetIngressSGs: make(map[string]bool),
		observedIngressSGs: make(map[string]bool),
	}
	for _, env := range envelopes {
		if env.FactKind != facts.AWSSecurityGroupRuleFactKind || env.IsTombstone {
			continue
		}
		if strings.TrimSpace(payloadString(env.Payload, "direction")) != "ingress" {
			continue
		}
		groupID := payloadString(env.Payload, "group_id")
		if groupID == "" {
			continue
		}
		index.observedIngressSGs[groupID] = true
		if payloadBool(env.Payload, "is_internet") || ec2RuleSourceIsInternet(env.Payload) {
			index.internetIngressSGs[groupID] = true
		}
	}
	return index
}

func ec2RuleSourceIsInternet(payload map[string]any) bool {
	sourceKind := payloadString(payload, "source_kind")
	sourceValue := payloadString(payload, "source_value")
	return (sourceKind == "cidr_ipv4" && sourceValue == "0.0.0.0/0") ||
		(sourceKind == "cidr_ipv6" && sourceValue == "::/0")
}

func addToSet(index map[string]map[string]struct{}, key, value string) {
	if index[key] == nil {
		index[key] = make(map[string]struct{})
	}
	index[key][value] = struct{}{}
}
