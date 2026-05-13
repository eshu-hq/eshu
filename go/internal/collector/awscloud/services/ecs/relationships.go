package ecs

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// taskDefinitionImageRelationships aggregates container evidence by image so
// relationship fact identity does not collapse same-image containers.
func taskDefinitionImageRelationships(
	boundary awscloud.Boundary,
	taskDefinition TaskDefinition,
) []awscloud.RelationshipObservation {
	taskDefinitionARN := strings.TrimSpace(taskDefinition.ARN)
	type imageEvidence struct {
		image          string
		containerNames []string
	}
	byImage := make(map[string]*imageEvidence)
	var orderedImages []string
	for _, container := range taskDefinition.Containers {
		image := strings.TrimSpace(container.Image)
		if image == "" {
			continue
		}
		evidence, ok := byImage[image]
		if !ok {
			evidence = &imageEvidence{image: image}
			byImage[image] = evidence
			orderedImages = append(orderedImages, image)
		}
		if name := strings.TrimSpace(container.Name); name != "" {
			evidence.containerNames = append(evidence.containerNames, name)
		}
	}

	observations := make([]awscloud.RelationshipObservation, 0, len(orderedImages))
	for _, image := range orderedImages {
		evidence := byImage[image]
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipECSTaskDefinitionUsesImage,
			SourceResourceID: taskDefinitionARN,
			SourceARN:        taskDefinitionARN,
			TargetResourceID: evidence.image,
			TargetType:       containerImageTargetType,
			Attributes: map[string]any{
				"container_names": cloneStrings(evidence.containerNames),
			},
			SourceRecordID: taskDefinitionARN + "#container-image#" + evidence.image,
		})
	}
	return observations
}

func taskNetworkInterfaceRelationships(
	boundary awscloud.Boundary,
	task Task,
) []awscloud.RelationshipObservation {
	taskARN := strings.TrimSpace(task.ARN)
	if taskARN == "" {
		return nil
	}
	var observations []awscloud.RelationshipObservation
	for _, networkInterface := range task.NetworkInterfaces {
		networkInterfaceID := strings.TrimSpace(networkInterface.NetworkInterfaceID)
		if networkInterfaceID == "" {
			continue
		}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipECSTaskUsesNetworkInterface,
			SourceResourceID: taskARN,
			SourceARN:        taskARN,
			TargetResourceID: networkInterfaceID,
			TargetType:       awscloud.ResourceTypeEC2NetworkInterface,
			Attributes: map[string]any{
				"mac_address":          strings.TrimSpace(networkInterface.MACAddress),
				"network_interface_id": networkInterfaceID,
				"private_ipv4_address": strings.TrimSpace(networkInterface.PrivateIPv4Address),
				"subnet_id":            strings.TrimSpace(networkInterface.SubnetID),
			},
			SourceRecordID: taskARN + "#network-interface#" + networkInterfaceID,
		})
	}
	return observations
}
