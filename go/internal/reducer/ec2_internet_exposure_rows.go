// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
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
) ([]map[string]any, ec2InternetExposureTally, []quarantinedFact, error) {
	tally := newEC2InternetExposureTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	var quarantined []quarantinedFact
	relationships, relationshipQuarantined, err := buildEC2InternetExposureRelationshipIndex(relationshipEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}
	quarantined = append(quarantined, relationshipQuarantined...)
	rules, ruleQuarantined, err := buildEC2InternetExposureRuleIndex(ruleEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}
	quarantined = append(quarantined, ruleQuarantined...)
	postures, postureQuarantined, err := sortedEC2InternetExposurePostures(postureEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}
	quarantined = append(quarantined, postureQuarantined...)
	byUID := make(map[string]map[string]any, len(postureEnvelopes))
	for _, item := range postures {
		if item.env.IsTombstone {
			tally.skipped[ec2InternetExposureSkipTombstone]++
			continue
		}
		uid, instanceID, ok := ec2InternetExposureIdentity(item.posture)
		if !ok {
			tally.skipped[ec2InternetExposureSkipMissingIdentity]++
			continue
		}
		decision := deriveEC2InternetExposureDecision(item.posture, instanceID, relationships, rules)
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
			"source_fact_id":   item.env.FactID,
		}
	}
	if len(byUID) == 0 {
		return nil, tally, quarantined, nil
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
	return rows, tally, quarantined, nil
}

// ec2InternetExposurePosture pairs a decoded ec2_instance_posture struct with
// its envelope so the extractor keeps the provenance scalars (FactID,
// IsTombstone) while reading posture fields from the typed struct.
type ec2InternetExposurePosture struct {
	env     facts.Envelope
	posture awsv1.EC2InstancePosture
}

func sortedEC2InternetExposurePostures(envelopes []facts.Envelope) ([]ec2InternetExposurePosture, []quarantinedFact, error) {
	postures := make([]ec2InternetExposurePosture, 0, len(envelopes))
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.EC2InstancePostureFactKind {
			continue
		}
		posture, err := decodeEC2InstancePosture(env)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return nil, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		postures = append(postures, ec2InternetExposurePosture{env: env, posture: posture})
	}
	sort.SliceStable(postures, func(i, j int) bool {
		left := derefString(postures[i].posture.InstanceID)
		right := derefString(postures[j].posture.InstanceID)
		if left != right {
			return left < right
		}
		return postures[i].env.FactID < postures[j].env.FactID
	})
	return postures, quarantined, nil
}

func ec2InternetExposureIdentity(posture awsv1.EC2InstancePosture) (uid, instanceID string, ok bool) {
	instanceID = derefString(posture.InstanceID)
	arn := derefString(posture.ARN)
	resourceID := instanceID
	if resourceID == "" {
		resourceID = arn
	}
	if resourceID == "" {
		return "", "", false
	}
	resourceType := derefString(posture.ResourceType)
	if resourceType == "" {
		resourceType = "aws_ec2_instance"
	}
	return cloudResourceUID(posture.AccountID, posture.Region, resourceType, resourceID), resourceID, true
}

func deriveEC2InternetExposureDecision(
	posture awsv1.EC2InstancePosture,
	instanceID string,
	relationships ec2InternetExposureRelationshipIndex,
	rules ec2InternetExposureRuleIndex,
) ec2InternetExposureDecision {
	publicIP := posture.PublicIPAssociated
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

func buildEC2InternetExposureRelationshipIndex(envelopes []facts.Envelope) (ec2InternetExposureRelationshipIndex, []quarantinedFact, error) {
	index := ec2InternetExposureRelationshipIndex{
		enisByInstance: make(map[string]map[string]struct{}),
		sgsByENI:       make(map[string]map[string]struct{}),
	}
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.AWSRelationshipFactKind || env.IsTombstone {
			continue
		}
		relationship, err := decodeAWSRelationship(env)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return ec2InternetExposureRelationshipIndex{}, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		sourceID := relationship.SourceResourceID
		targetID := relationship.TargetResourceID
		targetType := derefString(relationship.TargetType)
		if sourceID == "" || targetID == "" {
			continue
		}
		switch relationship.RelationshipType {
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
	return index, quarantined, nil
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

func buildEC2InternetExposureRuleIndex(envelopes []facts.Envelope) (ec2InternetExposureRuleIndex, []quarantinedFact, error) {
	index := ec2InternetExposureRuleIndex{
		internetIngressSGs: make(map[string]bool),
		observedIngressSGs: make(map[string]bool),
	}
	var quarantined []quarantinedFact
	for _, env := range envelopes {
		if env.FactKind != facts.AWSSecurityGroupRuleFactKind || env.IsTombstone {
			continue
		}
		rule, err := decodeAWSSecurityGroupRule(env)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return ec2InternetExposureRuleIndex{}, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if strings.TrimSpace(rule.Direction) != "ingress" {
			continue
		}
		if rule.GroupID == "" {
			continue
		}
		index.observedIngressSGs[rule.GroupID] = true
		isInternet := rule.IsInternet != nil && *rule.IsInternet
		if isInternet || ec2RuleSourceIsInternet(rule) {
			index.internetIngressSGs[rule.GroupID] = true
		}
	}
	return index, quarantined, nil
}

func ec2RuleSourceIsInternet(rule awsv1.SecurityGroupRule) bool {
	return (rule.SourceKind == "cidr_ipv4" && rule.SourceValue == "0.0.0.0/0") ||
		(rule.SourceKind == "cidr_ipv6" && rule.SourceValue == "::/0")
}

func addToSet(index map[string]map[string]struct{}, key, value string) {
	if index[key] == nil {
		index[key] = make(map[string]struct{})
	}
	index[key][value] = struct{}{}
}
