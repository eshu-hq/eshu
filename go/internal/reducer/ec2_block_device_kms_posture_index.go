// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

type ec2BlockDeviceKMSVolume struct {
	id          string
	arn         string
	encrypted   *bool
	attachments []ec2BlockDeviceKMSAttachment
}

type ec2BlockDeviceKMSAttachment struct {
	instanceID string
	state      string
}

type ec2BlockDeviceKMSKey struct {
	id         string
	keyManager string
}

type ec2BlockDeviceKMSIndex struct {
	volumesByID          map[string]ec2BlockDeviceKMSVolume
	ambiguousVolumesByID map[string]struct{}
	keysByIdentity       map[string]ec2BlockDeviceKMSKey
	kmsByVolume          map[string]string
	ambiguousKMSByVolume map[string]struct{}
}

func buildEC2BlockDeviceKMSIndex(
	resourceEnvelopes []facts.Envelope,
	relationshipEnvelopes []facts.Envelope,
) ec2BlockDeviceKMSIndex {
	index := ec2BlockDeviceKMSIndex{
		volumesByID:          make(map[string]ec2BlockDeviceKMSVolume, len(resourceEnvelopes)),
		ambiguousVolumesByID: make(map[string]struct{}),
		keysByIdentity:       make(map[string]ec2BlockDeviceKMSKey, len(resourceEnvelopes)),
		kmsByVolume:          make(map[string]string, len(relationshipEnvelopes)),
		ambiguousKMSByVolume: make(map[string]struct{}),
	}
	for _, env := range resourceEnvelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		switch payloadString(env.Payload, "resource_type") {
		case ec2BlockDeviceKMSResourceTypeVolume:
			volume, ok := ec2BlockDeviceKMSVolumeFromEnvelope(env)
			if ok {
				index.indexVolume(volume)
			}
		case ec2BlockDeviceKMSResourceTypeKey:
			key, identities, ok := ec2BlockDeviceKMSKeyFromEnvelope(env)
			if ok {
				for _, identity := range identities {
					index.keysByIdentity[identity] = key
				}
			}
		}
	}
	for _, env := range relationshipEnvelopes {
		if env.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if payloadString(env.Payload, "relationship_type") != ec2BlockDeviceKMSRelationshipType {
			continue
		}
		targetID := firstTrimmed(
			payloadString(env.Payload, "target_arn"),
			payloadString(env.Payload, "target_resource_id"),
		)
		if targetID == "" {
			continue
		}
		for _, sourceID := range []string{
			payloadString(env.Payload, "source_resource_id"),
			payloadString(env.Payload, "source_arn"),
		} {
			index.indexKMSRelationship(sourceID, targetID)
		}
	}
	return index
}

func (i *ec2BlockDeviceKMSIndex) indexVolume(volume ec2BlockDeviceKMSVolume) {
	for _, identity := range uniqueSortedStrings([]string{volume.id, volume.arn}) {
		if identity == "" {
			continue
		}
		if _, ambiguous := i.ambiguousVolumesByID[identity]; ambiguous {
			continue
		}
		existing, exists := i.volumesByID[identity]
		if !exists {
			i.volumesByID[identity] = volume
			continue
		}
		if ec2BlockDeviceKMSVolumesEqual(existing, volume) {
			continue
		}
		delete(i.volumesByID, identity)
		i.ambiguousVolumesByID[identity] = struct{}{}
	}
}

func (i ec2BlockDeviceKMSIndex) resolveVolume(identity string) (ec2BlockDeviceKMSVolume, bool, string) {
	identity = strings.TrimSpace(identity)
	if identity == "" {
		return ec2BlockDeviceKMSVolume{}, false, ""
	}
	if _, ambiguous := i.ambiguousVolumesByID[identity]; ambiguous {
		return ec2BlockDeviceKMSVolume{}, false, ec2BlockDeviceKMSReasonAmbiguousVolumeFact
	}
	volume, ok := i.volumesByID[identity]
	return volume, ok, ""
}

func (i *ec2BlockDeviceKMSIndex) indexKMSRelationship(sourceID, targetID string) {
	sourceID = strings.TrimSpace(sourceID)
	targetID = strings.TrimSpace(targetID)
	if sourceID == "" || targetID == "" {
		return
	}
	if _, ambiguous := i.ambiguousKMSByVolume[sourceID]; ambiguous {
		return
	}
	existing, exists := i.kmsByVolume[sourceID]
	if !exists {
		i.kmsByVolume[sourceID] = targetID
		return
	}
	if existing == targetID {
		return
	}
	delete(i.kmsByVolume, sourceID)
	i.ambiguousKMSByVolume[sourceID] = struct{}{}
}

func ec2BlockDeviceKMSVolumeFromEnvelope(env facts.Envelope) (ec2BlockDeviceKMSVolume, bool) {
	resourceID := firstTrimmed(payloadString(env.Payload, "resource_id"), payloadString(env.Payload, "arn"))
	if resourceID == "" {
		return ec2BlockDeviceKMSVolume{}, false
	}
	attrs := payloadAttributes(env.Payload)
	return ec2BlockDeviceKMSVolume{
		id:          resourceID,
		arn:         payloadString(env.Payload, "arn"),
		encrypted:   payloadAttributeBool(attrs, "encrypted"),
		attachments: ec2BlockDeviceKMSAttachments(attrs),
	}, true
}

func ec2BlockDeviceKMSKeyFromEnvelope(env facts.Envelope) (ec2BlockDeviceKMSKey, []string, bool) {
	resourceID := firstTrimmed(payloadString(env.Payload, "resource_id"), payloadString(env.Payload, "arn"))
	if resourceID == "" {
		return ec2BlockDeviceKMSKey{}, nil, false
	}
	attrs := payloadAttributes(env.Payload)
	key := ec2BlockDeviceKMSKey{
		id:         firstTrimmed(payloadString(env.Payload, "arn"), resourceID),
		keyManager: strings.ToUpper(payloadString(attrs, "key_manager")),
	}
	identities := []string{resourceID, payloadString(env.Payload, "arn")}
	identities = append(identities, payloadStrings(env.Payload, "", "correlation_anchors")...)
	return key, uniqueSortedStrings(identities), true
}

func ec2BlockDeviceKMSVolumeKeyID(volume ec2BlockDeviceKMSVolume, index ec2BlockDeviceKMSIndex) (string, string) {
	keyIDs := make([]string, 0, 2)
	for _, volumeIdentity := range uniqueSortedStrings([]string{volume.id, volume.arn}) {
		if _, ambiguous := index.ambiguousKMSByVolume[volumeIdentity]; ambiguous {
			return "", ec2BlockDeviceKMSReasonAmbiguousKMSRelationship
		}
		if keyID := index.kmsByVolume[volumeIdentity]; keyID != "" {
			keyIDs = append(keyIDs, keyID)
		}
	}
	keyIDs = uniqueSortedStrings(keyIDs)
	switch len(keyIDs) {
	case 0:
		return "", ec2BlockDeviceKMSReasonMissingKMSRelationship
	case 1:
		return keyIDs[0], ""
	default:
		return "", ec2BlockDeviceKMSReasonAmbiguousKMSRelationship
	}
}

func ec2BlockDeviceKMSAttachments(attrs map[string]any) []ec2BlockDeviceKMSAttachment {
	raw, ok := attrs["attachments"]
	if !ok || raw == nil {
		return nil
	}
	output := make([]ec2BlockDeviceKMSAttachment, 0)
	appendAttachment := func(entry map[string]any) {
		output = append(output, ec2BlockDeviceKMSAttachment{
			instanceID: payloadString(entry, "instance_id"),
			state:      payloadString(entry, "state"),
		})
	}
	switch typed := raw.(type) {
	case []map[string]any:
		for _, entry := range typed {
			appendAttachment(entry)
		}
	case []any:
		for _, entry := range typed {
			if entryMap, ok := entry.(map[string]any); ok {
				appendAttachment(entryMap)
			}
		}
	}
	return output
}

func ec2BlockDeviceKMSVolumesEqual(left, right ec2BlockDeviceKMSVolume) bool {
	if left.id != right.id || left.arn != right.arn {
		return false
	}
	if !boolPointersEqual(left.encrypted, right.encrypted) {
		return false
	}
	return ec2BlockDeviceKMSAttachmentsEqual(left.attachments, right.attachments)
}

func boolPointersEqual(left, right *bool) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func ec2BlockDeviceKMSAttachmentsEqual(left, right []ec2BlockDeviceKMSAttachment) bool {
	if len(left) != len(right) {
		return false
	}
	leftKeys := ec2BlockDeviceKMSAttachmentKeys(left)
	rightKeys := ec2BlockDeviceKMSAttachmentKeys(right)
	for idx := range leftKeys {
		if leftKeys[idx] != rightKeys[idx] {
			return false
		}
	}
	return true
}

func ec2BlockDeviceKMSAttachmentKeys(attachments []ec2BlockDeviceKMSAttachment) []string {
	keys := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		keys = append(keys, attachment.instanceID+"\x00"+attachment.state)
	}
	sort.Strings(keys)
	return keys
}
