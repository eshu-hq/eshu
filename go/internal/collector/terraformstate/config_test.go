package terraformstate

import "testing"

func TestParseDiscoveryConfigCarriesTargetScopeIDs(t *testing.T) {
	t.Parallel()

	config, err := ParseDiscoveryConfig(`{
		"discovery": {
			"seeds": [
				{
					"kind": "s3",
					"target_scope_id": "aws-prod",
					"bucket": "tfstate-prod",
					"key": "services/api/terraform.tfstate",
					"region": "us-east-1"
				}
			],
			"local_state_candidates": {
				"mode": "approved_candidates",
				"approved": [
					{
						"repo_id": "platform-infra",
						"path": "env/prod/terraform.tfstate",
						"target_scope_id": "aws-prod"
					}
				]
			}
		}
	}`)
	if err != nil {
		t.Fatalf("ParseDiscoveryConfig() error = %v, want nil", err)
	}

	if got, want := config.Seeds[0].TargetScopeID, "aws-prod"; got != want {
		t.Fatalf("seed TargetScopeID = %q, want %q", got, want)
	}
	if got, want := config.LocalStateCandidates.Approved[0].TargetScopeID, "aws-prod"; got != want {
		t.Fatalf("approved TargetScopeID = %q, want %q", got, want)
	}
}
