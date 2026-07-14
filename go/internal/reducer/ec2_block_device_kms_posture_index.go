// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
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
) (ec2BlockDeviceKMSIndex, []quarantinedFact, error) {
	index := ec2BlockDeviceKMSIndex{
		volumesByID:          make(map[string]ec2BlockDeviceKMSVolume, len(resourceEnvelopes)),
		ambiguousVolumesByID: make(map[string]struct{}),
		keysByIdentity:       make(map[string]ec2BlockDeviceKMSKey, len(resourceEnvelopes)),
		kmsByVolume:          make(map[string]string, len(relationshipEnvelopes)),
		ambiguousKMSByVolume: make(map[string]struct{}),
	}
	var quarantined []quarantinedFact
	for _, env := range resourceEnvelopes {
		if env.FactKind != facts.AWSResourceFactKind {
			continue
		}
		resource, err := decodeAWSResource(env)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return ec2BlockDeviceKMSIndex{}, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		switch resource.ResourceType {
		case ec2BlockDeviceKMSResourceTypeVolume:
			volume, ok, attrErr := ec2BlockDeviceKMSVolumeFromResource(resource)
			if attrErr != nil {
				quarantined = append(quarantined, quarantinedAttributeShapeFact(env, attrErr))
				continue
			}
			if ok {
				index.indexVolume(volume)
			}
		case ec2BlockDeviceKMSResourceTypeKey:
			key, identities, ok, attrErr := ec2BlockDeviceKMSKeyFromResource(resource)
			if attrErr != nil {
				quarantined = append(quarantined, quarantinedAttributeShapeFact(env, attrErr))
				continue
			}
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
		relationship, err := decodeAWSRelationship(env)
		if err != nil {
			q, ok, fatal := partitionDecodeFailures(env, err)
			if fatal != nil {
				return ec2BlockDeviceKMSIndex{}, nil, fatal
			}
			if ok {
				quarantined = append(quarantined, q)
			}
			continue
		}
		if relationship.RelationshipType != ec2BlockDeviceKMSRelationshipType {
			continue
		}
		targetID := firstTrimmed(
			derefString(relationship.TargetARN),
			relationship.TargetResourceID,
		)
		if targetID == "" {
			continue
		}
		for _, sourceID := range []string{
			relationship.SourceResourceID,
			derefString(relationship.SourceARN),
		} {
			index.indexKMSRelationship(sourceID, targetID)
		}
	}
	return index, quarantined, nil
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

func ec2BlockDeviceKMSVolumeFromResource(resource awsv1.Resource) (ec2BlockDeviceKMSVolume, bool, error) {
	arn := derefString(resource.ARN)
	resourceID := firstTrimmed(resource.ResourceID, arn)
	if resourceID == "" {
		return ec2BlockDeviceKMSVolume{}, false, nil
	}
	// The per-volume encryption/attachment detail is a service-specific nested
	// "attributes" object carried in the decoded struct's Attributes
	// pass-through, not a named identity field. DecodeResourceEC2VolumeAttributes
	// validates the JSON type of each field it reads (encrypted must be a bool,
	// every attachment must be an object with string instance_id/state); a
	// present-but-malformed value is a decode error the caller must dead-letter
	// rather than silently treat as "not encrypted"/"no attachments" — a wrong
	// encryption posture reading would be a real security-posture inaccuracy.
	attrs, err := awsv1.DecodeResourceEC2VolumeAttributes(resource)
	if err != nil {
		return ec2BlockDeviceKMSVolume{}, false, err
	}
	return ec2BlockDeviceKMSVolume{
		id:          resourceID,
		arn:         arn,
		encrypted:   attrs.Encrypted,
		attachments: ec2BlockDeviceKMSAttachmentsFromTyped(attrs.Attachments),
	}, true, nil
}

func ec2BlockDeviceKMSKeyFromResource(resource awsv1.Resource) (ec2BlockDeviceKMSKey, []string, bool, error) {
	arn := derefString(resource.ARN)
	resourceID := firstTrimmed(resource.ResourceID, arn)
	if resourceID == "" {
		return ec2BlockDeviceKMSKey{}, nil, false, nil
	}
	attrs, err := awsv1.DecodeResourceKMSKeyAttributes(resource)
	if err != nil {
		return ec2BlockDeviceKMSKey{}, nil, false, err
	}
	key := ec2BlockDeviceKMSKey{
		id:         firstTrimmed(arn, resourceID),
		keyManager: strings.ToUpper(attrs.KeyManager),
	}
	identities := []string{resourceID, arn}
	identities = append(identities, resource.CorrelationAnchors...)
	return key, uniqueSortedStrings(identities), true, nil
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

// ec2BlockDeviceKMSAttachmentsFromTyped adapts the typed
// awsv1.EC2VolumeAttachment slice DecodeResourceEC2VolumeAttributes returns
// to this file's local ec2BlockDeviceKMSAttachment shape.
func ec2BlockDeviceKMSAttachmentsFromTyped(attachments []awsv1.EC2VolumeAttachment) []ec2BlockDeviceKMSAttachment {
	if attachments == nil {
		return nil
	}
	output := make([]ec2BlockDeviceKMSAttachment, 0, len(attachments))
	for _, entry := range attachments {
		output = append(output, ec2BlockDeviceKMSAttachment{
			instanceID: entry.InstanceID,
			state:      entry.State,
		})
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
