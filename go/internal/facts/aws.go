package facts

import "slices"

const (
	// AWSResourceFactKind identifies one resource reported by an AWS API.
	AWSResourceFactKind = "aws_resource"
	// AWSRelationshipFactKind identifies one relationship reported by AWS APIs.
	AWSRelationshipFactKind = "aws_relationship"
	// AWSTagObservationFactKind identifies one raw AWS tag observation.
	AWSTagObservationFactKind = "aws_tag_observation"
	// AWSDNSRecordFactKind identifies one Route53 DNS record observation.
	AWSDNSRecordFactKind = "aws_dns_record"
	// AWSImageReferenceFactKind identifies one ECR image reference observation.
	AWSImageReferenceFactKind = "aws_image_reference"
	// AWSWarningFactKind identifies one non-fatal AWS scanner warning.
	AWSWarningFactKind = "aws_warning"

	// AWSResourceSchemaVersion is the first AWS resource fact schema.
	AWSResourceSchemaVersion = "1.0.0"
	// AWSRelationshipSchemaVersion is the first AWS relationship fact schema.
	AWSRelationshipSchemaVersion = "1.0.0"
	// AWSTagObservationSchemaVersion is the first AWS tag observation schema.
	AWSTagObservationSchemaVersion = "1.0.0"
	// AWSDNSRecordSchemaVersion is the first AWS DNS record schema.
	AWSDNSRecordSchemaVersion = "1.0.0"
	// AWSImageReferenceSchemaVersion is the first AWS image reference schema.
	AWSImageReferenceSchemaVersion = "1.0.0"
	// AWSWarningSchemaVersion is the first AWS warning fact schema.
	AWSWarningSchemaVersion = "1.0.0"
)

var awsFactKinds = []string{
	AWSResourceFactKind,
	AWSRelationshipFactKind,
	AWSTagObservationFactKind,
	AWSDNSRecordFactKind,
	AWSImageReferenceFactKind,
	AWSWarningFactKind,
}

var awsSchemaVersions = map[string]string{
	AWSResourceFactKind:       AWSResourceSchemaVersion,
	AWSRelationshipFactKind:   AWSRelationshipSchemaVersion,
	AWSTagObservationFactKind: AWSTagObservationSchemaVersion,
	AWSDNSRecordFactKind:      AWSDNSRecordSchemaVersion,
	AWSImageReferenceFactKind: AWSImageReferenceSchemaVersion,
	AWSWarningFactKind:        AWSWarningSchemaVersion,
}

// AWSFactKinds returns the accepted AWS fact kinds in their emission order.
func AWSFactKinds() []string {
	return slices.Clone(awsFactKinds)
}

// AWSSchemaVersion returns the schema version for an AWS fact kind.
func AWSSchemaVersion(factKind string) (string, bool) {
	version, ok := awsSchemaVersions[factKind]
	return version, ok
}
