// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	ec2BlockDeviceKMSResourceTypeInstance = "aws_ec2_instance"
	ec2BlockDeviceKMSResourceTypeVolume   = "aws_ec2_volume"
	ec2BlockDeviceKMSResourceTypeKey      = "aws_kms_key"
	ec2BlockDeviceKMSRelationshipType     = "ec2_volume_uses_kms_key"

	ec2BlockDeviceKMSStateEncrypted    = "encrypted"
	ec2BlockDeviceKMSStateNotEncrypted = "not_encrypted"
	ec2BlockDeviceKMSStateMixed        = "mixed"
	ec2BlockDeviceKMSStateUnknown      = "unknown"

	ec2BlockDeviceKMSReasonAllVolumesCustomerManaged = "all_volumes_customer_managed_kms"
	ec2BlockDeviceKMSReasonAllVolumesUnencrypted     = "all_volumes_unencrypted"
	ec2BlockDeviceKMSReasonMixedEncryption           = "mixed_encryption"
	ec2BlockDeviceKMSReasonNoBlockDevices            = "no_block_devices"
	ec2BlockDeviceKMSReasonMissingVolumeFact         = "missing_volume_fact"
	ec2BlockDeviceKMSReasonMissingKMSRelationship    = "missing_kms_relationship"
	ec2BlockDeviceKMSReasonMissingKMSKeyFact         = "missing_kms_key_fact"
	ec2BlockDeviceKMSReasonAWSManagedOrDefaultKey    = "aws_managed_or_default_key"
	ec2BlockDeviceKMSReasonVolumeDetached            = "volume_detached"
	ec2BlockDeviceKMSReasonAttachmentMismatch        = "attachment_mismatch"
	ec2BlockDeviceKMSReasonEncryptionUnknown         = "volume_encryption_unknown"
	ec2BlockDeviceKMSReasonAmbiguousVolumeFact       = "ambiguous_volume_fact"
	ec2BlockDeviceKMSReasonAmbiguousKMSRelationship  = "ambiguous_kms_relationship"
	ec2BlockDeviceKMSSkipSourceUnresolved            = "source_unresolved"
	ec2BlockDeviceKMSSkipTombstone                   = "tombstone"
)

type ec2BlockDeviceKMSPostureTally struct {
	decisions       map[string]int
	decisionReasons map[ec2BlockDeviceKMSDecisionKey]int
	reasons         map[string]int
	skipped         map[string]int
}

type ec2BlockDeviceKMSDecisionKey struct {
	outcome string
	reason  string
}

func newEC2BlockDeviceKMSPostureTally() ec2BlockDeviceKMSPostureTally {
	return ec2BlockDeviceKMSPostureTally{
		decisions:       make(map[string]int),
		decisionReasons: make(map[ec2BlockDeviceKMSDecisionKey]int),
		reasons:         make(map[string]int),
		skipped:         make(map[string]int),
	}
}

func (t ec2BlockDeviceKMSPostureTally) totalSkipped() int {
	total := 0
	for _, count := range t.skipped {
		total += count
	}
	return total
}

type ec2BlockDeviceKMSDecision struct {
	state                  string
	reason                 string
	volumeCount            int64
	encryptedVolumeCount   int64
	unencryptedVolumeCount int64
	unresolvedVolumeCount  int64
	kmsKeyCount            int64
	volumeIDs              []string
	kmsKeyIDs              []string
}

// ExtractEC2BlockDeviceKMSPostureRows derives conservative EC2 block-device KMS
// posture rows from EC2 posture, EBS volume, KMS key, and volume->KMS relationship
// facts. It never fabricates nodes: EC2 node existence is fenced by the handler
// readiness gate, EBS/KMS facts must be present in the same scope generation, and
// missing or ambiguous evidence produces state=unknown instead of a safe value.
func ExtractEC2BlockDeviceKMSPostureRows(
	resourceEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
	postureEnvelopes []facts.Envelope,
) ([]map[string]any, ec2BlockDeviceKMSPostureTally, error) {
	tally := newEC2BlockDeviceKMSPostureTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally, nil
	}

	index, err := buildEC2BlockDeviceKMSIndex(resourceEnvelopes, relationshipEnvelopes)
	if err != nil {
		return nil, tally, err
	}
	postures := sortedEC2BlockDeviceKMSPostures(postureEnvelopes)
	byUID := make(map[string]map[string]any, len(postures))
	for _, env := range postures {
		if env.IsTombstone {
			tally.skipped[ec2BlockDeviceKMSSkipTombstone]++
			continue
		}
		uid, ok := ec2BlockDeviceKMSSourceUID(env)
		if !ok {
			tally.skipped[ec2BlockDeviceKMSSkipSourceUnresolved]++
			continue
		}
		decision := deriveEC2BlockDeviceKMSDecision(env, index)
		byUID[uid] = map[string]any{
			"uid":                      uid,
			"state":                    decision.state,
			"reason":                   decision.reason,
			"volume_count":             decision.volumeCount,
			"encrypted_volume_count":   decision.encryptedVolumeCount,
			"unencrypted_volume_count": decision.unencryptedVolumeCount,
			"unresolved_volume_count":  decision.unresolvedVolumeCount,
			"kms_key_count":            decision.kmsKeyCount,
			"volume_ids":               decision.volumeIDs,
			"kms_key_ids":              decision.kmsKeyIDs,
			"source_fact_id":           env.FactID,
		}
	}

	if len(byUID) == 0 {
		return nil, tally, nil
	}
	uids := make([]string, 0, len(byUID))
	for uid := range byUID {
		uids = append(uids, uid)
	}
	sort.Strings(uids)
	rows := make([]map[string]any, 0, len(uids))
	for _, uid := range uids {
		row := byUID[uid]
		state, _ := row["state"].(string)
		reason, _ := row["reason"].(string)
		tally.decisions[state]++
		tally.reasons[reason]++
		tally.decisionReasons[ec2BlockDeviceKMSDecisionKey{outcome: state, reason: reason}]++
		rows = append(rows, row)
	}
	return rows, tally, nil
}

func deriveEC2BlockDeviceKMSDecision(
	env facts.Envelope,
	index ec2BlockDeviceKMSIndex,
) ec2BlockDeviceKMSDecision {
	volumeIDs := ec2BlockDeviceKMSVolumeIDs(env.Payload)
	decision := ec2BlockDeviceKMSDecision{volumeIDs: volumeIDs}
	if len(volumeIDs) == 0 {
		decision.state = ec2BlockDeviceKMSStateUnknown
		decision.reason = ec2BlockDeviceKMSReasonNoBlockDevices
		return decision
	}
	decision.volumeCount = int64(len(volumeIDs))
	kmsKeys := make([]string, 0, len(volumeIDs))
	reason := ""
	for _, volumeID := range volumeIDs {
		volume, ok, volumeReason := index.resolveVolume(volumeID)
		if volumeReason != "" {
			decision.unresolvedVolumeCount++
			reason = firstTrimmed(reason, volumeReason)
			continue
		}
		if !ok {
			decision.unresolvedVolumeCount++
			reason = firstTrimmed(reason, ec2BlockDeviceKMSReasonMissingVolumeFact)
			continue
		}
		if attachmentReason := ec2BlockDeviceKMSAttachmentReason(env, volume); attachmentReason != "" {
			decision.unresolvedVolumeCount++
			reason = firstTrimmed(reason, attachmentReason)
			continue
		}
		if volume.encrypted == nil {
			decision.unresolvedVolumeCount++
			reason = firstTrimmed(reason, ec2BlockDeviceKMSReasonEncryptionUnknown)
			continue
		}
		if !*volume.encrypted {
			decision.unencryptedVolumeCount++
			continue
		}
		keyID, keyReason := ec2BlockDeviceKMSVolumeKeyID(volume, index)
		if keyReason != "" {
			decision.unresolvedVolumeCount++
			reason = firstTrimmed(reason, keyReason)
			continue
		}
		key, ok := index.keysByIdentity[keyID]
		if !ok {
			decision.unresolvedVolumeCount++
			reason = firstTrimmed(reason, ec2BlockDeviceKMSReasonMissingKMSKeyFact)
			continue
		}
		if key.keyManager != "CUSTOMER" {
			decision.unresolvedVolumeCount++
			reason = firstTrimmed(reason, ec2BlockDeviceKMSReasonAWSManagedOrDefaultKey)
			continue
		}
		decision.encryptedVolumeCount++
		kmsKeys = append(kmsKeys, key.id)
	}
	decision.kmsKeyIDs = uniqueSortedStrings(kmsKeys)
	decision.kmsKeyCount = int64(len(decision.kmsKeyIDs))
	decision.state, decision.reason = ec2BlockDeviceKMSAggregate(decision, reason)
	return decision
}

func ec2BlockDeviceKMSAggregate(decision ec2BlockDeviceKMSDecision, unknownReason string) (string, string) {
	if decision.unresolvedVolumeCount > 0 {
		return ec2BlockDeviceKMSStateUnknown, firstTrimmed(unknownReason, ec2BlockDeviceKMSReasonMissingVolumeFact)
	}
	if decision.encryptedVolumeCount > 0 && decision.unencryptedVolumeCount > 0 {
		return ec2BlockDeviceKMSStateMixed, ec2BlockDeviceKMSReasonMixedEncryption
	}
	if decision.unencryptedVolumeCount > 0 {
		return ec2BlockDeviceKMSStateNotEncrypted, ec2BlockDeviceKMSReasonAllVolumesUnencrypted
	}
	if decision.encryptedVolumeCount > 0 {
		return ec2BlockDeviceKMSStateEncrypted, ec2BlockDeviceKMSReasonAllVolumesCustomerManaged
	}
	return ec2BlockDeviceKMSStateUnknown, ec2BlockDeviceKMSReasonNoBlockDevices
}

func ec2BlockDeviceKMSAttachmentReason(env facts.Envelope, volume ec2BlockDeviceKMSVolume) string {
	if len(volume.attachments) == 0 {
		return ec2BlockDeviceKMSReasonVolumeDetached
	}
	instanceID := payloadString(env.Payload, "instance_id")
	for _, attachment := range volume.attachments {
		if attachment.instanceID != instanceID {
			continue
		}
		if attachment.state == "" || strings.EqualFold(attachment.state, "attached") {
			return ""
		}
		return ec2BlockDeviceKMSReasonVolumeDetached
	}
	return ec2BlockDeviceKMSReasonAttachmentMismatch
}

func ec2BlockDeviceKMSVolumeIDs(payload map[string]any) []string {
	raw, ok := payload["block_devices"]
	if !ok || raw == nil {
		return nil
	}
	values := make([]string, 0)
	switch typed := raw.(type) {
	case []map[string]any:
		for _, device := range typed {
			values = append(values, payloadString(device, "volume_id"))
		}
	case []any:
		for _, device := range typed {
			if deviceMap, ok := device.(map[string]any); ok {
				values = append(values, payloadString(deviceMap, "volume_id"))
			}
		}
	}
	return uniqueSortedStrings(values)
}

func sortedEC2BlockDeviceKMSPostures(envelopes []facts.Envelope) []facts.Envelope {
	postures := make([]facts.Envelope, 0, len(envelopes))
	for _, env := range envelopes {
		if env.FactKind == facts.EC2InstancePostureFactKind {
			postures = append(postures, env)
		}
	}
	sort.SliceStable(postures, func(i, j int) bool {
		leftUID, _ := ec2BlockDeviceKMSSourceUID(postures[i])
		rightUID, _ := ec2BlockDeviceKMSSourceUID(postures[j])
		if leftUID != rightUID {
			return leftUID < rightUID
		}
		return postures[i].FactID < postures[j].FactID
	})
	return postures
}

func ec2BlockDeviceKMSSourceUID(env facts.Envelope) (string, bool) {
	accountID := payloadString(env.Payload, "account_id")
	region := payloadString(env.Payload, "region")
	instanceID := payloadString(env.Payload, "instance_id")
	arn := payloadString(env.Payload, "arn")
	resourceID := firstTrimmed(instanceID, arn)
	if resourceID == "" {
		return "", false
	}
	resourceType := firstTrimmed(payloadString(env.Payload, "resource_type"), ec2BlockDeviceKMSResourceTypeInstance)
	return cloudResourceUID(accountID, region, resourceType, resourceID), true
}

func payloadAttributes(payload map[string]any) map[string]any {
	raw, ok := payload["attributes"]
	if !ok || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	default:
		return nil
	}
}

func payloadAttributeBool(payload map[string]any, key string) *bool {
	if payload == nil {
		return nil
	}
	value, ok := payloadBoolPointerValue(payload, key)
	if !ok {
		return nil
	}
	return &value
}

func firstTrimmed(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}
