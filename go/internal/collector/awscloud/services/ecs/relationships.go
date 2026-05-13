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
