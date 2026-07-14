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
) ([]map[string]any, ec2BlockDeviceKMSPostureTally, []quarantinedFact, error) {
	tally := newEC2BlockDeviceKMSPostureTally()
	if len(postureEnvelopes) == 0 {
		return nil, tally, nil, nil
	}

	var quarantined []quarantinedFact
	index, indexQuarantined, err := buildEC2BlockDeviceKMSIndex(resourceEnvelopes, relationshipEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}
	quarantined = append(quarantined, indexQuarantined...)
	postures, postureQuarantined, err := sortedEC2BlockDeviceKMSPostures(postureEnvelopes)
	if err != nil {
		return nil, tally, nil, err
	}
	quarantined = append(quarantined, postureQuarantined...)
	byUID := make(map[string]map[string]any, len(postures))
	for _, item := range postures {
		if item.env.IsTombstone {
			tally.skipped[ec2BlockDeviceKMSSkipTombstone]++
			continue
		}
		uid, ok := ec2BlockDeviceKMSSourceUID(item.posture)
		if !ok {
			tally.skipped[ec2BlockDeviceKMSSkipSourceUnresolved]++
			continue
		}
		decision := deriveEC2BlockDeviceKMSDecision(item.posture, index)
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
			"source_fact_id":           item.env.FactID,
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
		row := byUID[uid]
		state, _ := row["state"].(string)
		reason, _ := row["reason"].(string)
		tally.decisions[state]++
		tally.reasons[reason]++
		tally.decisionReasons[ec2BlockDeviceKMSDecisionKey{outcome: state, reason: reason}]++
		rows = append(rows, row)
	}
	return rows, tally, quarantined, nil
}

func deriveEC2BlockDeviceKMSDecision(
	posture awsv1.EC2InstancePosture,
	index ec2BlockDeviceKMSIndex,
) ec2BlockDeviceKMSDecision {
	volumeIDs := ec2BlockDeviceKMSVolumeIDs(posture)
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
		if attachmentReason := ec2BlockDeviceKMSAttachmentReason(posture, volume); attachmentReason != "" {
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

func ec2BlockDeviceKMSAttachmentReason(posture awsv1.EC2InstancePosture, volume ec2BlockDeviceKMSVolume) string {
	if len(volume.attachments) == 0 {
		return ec2BlockDeviceKMSReasonVolumeDetached
	}
	instanceID := derefString(posture.InstanceID)
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

// ec2BlockDeviceKMSVolumeIDs returns the sorted, deduplicated volume ids from a
// decoded ec2_instance_posture struct's typed BlockDevices, replacing the raw
// payload["block_devices"] []any/[]map traversal with the typed slice.
func ec2BlockDeviceKMSVolumeIDs(posture awsv1.EC2InstancePosture) []string {
	if len(posture.BlockDevices) == 0 {
		return nil
	}
	values := make([]string, 0, len(posture.BlockDevices))
	for _, device := range posture.BlockDevices {
		values = append(values, derefString(device.VolumeID))
	}
	return uniqueSortedStrings(values)
}

// ec2BlockDeviceKMSPostureItem pairs a decoded ec2_instance_posture struct with
// its envelope so the extractor keeps the provenance scalars while reading
// posture fields from the typed struct.
type ec2BlockDeviceKMSPostureItem struct {
	env     facts.Envelope
	posture awsv1.EC2InstancePosture
}

func sortedEC2BlockDeviceKMSPostures(envelopes []facts.Envelope) ([]ec2BlockDeviceKMSPostureItem, []quarantinedFact, error) {
	postures := make([]ec2BlockDeviceKMSPostureItem, 0, len(envelopes))
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
		postures = append(postures, ec2BlockDeviceKMSPostureItem{env: env, posture: posture})
	}
	sort.SliceStable(postures, func(i, j int) bool {
		leftUID, _ := ec2BlockDeviceKMSSourceUID(postures[i].posture)
		rightUID, _ := ec2BlockDeviceKMSSourceUID(postures[j].posture)
		if leftUID != rightUID {
			return leftUID < rightUID
		}
		return postures[i].env.FactID < postures[j].env.FactID
	})
	return postures, quarantined, nil
}

func ec2BlockDeviceKMSSourceUID(posture awsv1.EC2InstancePosture) (string, bool) {
	instanceID := derefString(posture.InstanceID)
	arn := derefString(posture.ARN)
	resourceID := firstTrimmed(instanceID, arn)
	if resourceID == "" {
		return "", false
	}
	resourceType := firstTrimmed(derefString(posture.ResourceType), ec2BlockDeviceKMSResourceTypeInstance)
	return cloudResourceUID(posture.AccountID, posture.Region, resourceType, resourceID), true
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
