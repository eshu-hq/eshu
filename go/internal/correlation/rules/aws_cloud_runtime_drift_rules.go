package rules

// AWSCloudRuntimeDriftPackName is the stable rule-pack identifier for the
// first AWS cloud-runtime drift slice.
const AWSCloudRuntimeDriftPackName = "aws_cloud_runtime_drift"

// AWS cloud-runtime drift rule names referenced by telemetry and tests.
const (
	AWSCloudRuntimeDriftRuleExtractARNKey      = "extract-arn-key"
	AWSCloudRuntimeDriftRuleMatchCloudState    = "match-cloud-state-config-by-arn"
	AWSCloudRuntimeDriftRuleDeriveFinding      = "derive-cloud-runtime-finding"
	AWSCloudRuntimeDriftRuleAdmitFinding       = "admit-cloud-runtime-finding"
	AWSCloudRuntimeDriftRuleExplainFinding     = "explain-cloud-runtime-finding"
	awsCloudRuntimeDriftMinAdmissionConfidence = 0.85
)

// AWSCloudRuntimeDriftRulePack returns the first-party rule pack for joining
// AWS runtime observations with Terraform state and Terraform config by ARN.
func AWSCloudRuntimeDriftRulePack() RulePack {
	return RulePack{
		Name:                   AWSCloudRuntimeDriftPackName,
		MinAdmissionConfidence: awsCloudRuntimeDriftMinAdmissionConfidence,
		RequiredEvidence: []EvidenceRequirement{
			{
				Name:     "cloud-resource-arn",
				MinCount: 1,
				MatchAll: []EvidenceSelector{
					{Field: EvidenceFieldEvidenceType, Value: "aws_cloud_resource_arn"},
				},
			},
		},
		Rules: []Rule{
			{Name: AWSCloudRuntimeDriftRuleExtractARNKey, Kind: RuleKindExtractKey, Priority: 10},
			{Name: AWSCloudRuntimeDriftRuleMatchCloudState, Kind: RuleKindMatch, Priority: 20, MaxMatches: 1},
			{Name: AWSCloudRuntimeDriftRuleDeriveFinding, Kind: RuleKindDerive, Priority: 30},
			{Name: AWSCloudRuntimeDriftRuleAdmitFinding, Kind: RuleKindAdmit, Priority: 40},
			{Name: AWSCloudRuntimeDriftRuleExplainFinding, Kind: RuleKindExplain, Priority: 50},
		},
	}
}
