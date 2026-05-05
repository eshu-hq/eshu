package relationships

import (
	"regexp"
	"strings"
)

var terraformSSMConfigPathPattern = regexp.MustCompile(`(?i)(?:parameter)?(/(?:configd|api)/([A-Za-z0-9._-]+)(?:/[A-Za-z0-9._${}*-]+)*)`)

type terraformIAMSSMConfigRead struct {
	ServiceName string
	Resource    string
	Actions     []string
}

// terraformIAMSSMConfigReadCandidates finds SSM parameter resources that are
// paired with read-only IAM actions and therefore describe config access.
func terraformIAMSSMConfigReadCandidates(content string) []terraformIAMSSMConfigRead {
	actions := terraformSSMReadActions(content)
	if len(actions) == 0 {
		return nil
	}
	matches := terraformSSMConfigPathPattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(matches))
	reads := make([]terraformIAMSSMConfigRead, 0, len(matches))
	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		resource := strings.TrimSpace(match[1])
		serviceName := strings.TrimSpace(match[2])
		if resource == "" || serviceName == "" {
			continue
		}
		key := strings.ToLower(resource)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		reads = append(reads, terraformIAMSSMConfigRead{
			ServiceName: serviceName,
			Resource:    resource,
			Actions:     append([]string(nil), actions...),
		})
	}
	return reads
}

// terraformSSMReadActions returns the read actions that make a config-path
// resource an access relationship instead of provisioning evidence.
func terraformSSMReadActions(content string) []string {
	lower := strings.ToLower(content)
	actions := make([]string, 0, 3)
	for _, action := range []string{
		"ssm:GetParameter",
		"ssm:GetParameters",
		"ssm:GetParametersByPath",
	} {
		if strings.Contains(lower, strings.ToLower(action)) {
			actions = append(actions, action)
		}
	}
	return actions
}

// isTerraformIAMConfigReadCandidate guards the broad Terraform config-path
// extractor from also emitting a misleading provisioning edge.
func isTerraformIAMConfigReadCandidate(candidate string, reads []terraformIAMSSMConfigRead) bool {
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if candidate == "" {
		return false
	}
	for _, read := range reads {
		if candidate == strings.ToLower(strings.TrimSpace(read.ServiceName)) {
			return true
		}
	}
	return false
}

// discoverTerraformIAMSSMConfigReadEvidence emits canonical repo evidence for
// service config reads granted through Terraform IAM policy resources.
func discoverTerraformIAMSSMConfigReadEvidence(
	sourceRepoID, filePath string,
	reads []terraformIAMSSMConfigRead,
	catalog []CatalogEntry,
	seen map[evidenceKey]struct{},
) []EvidenceFact {
	if len(reads) == 0 {
		return nil
	}
	var evidence []EvidenceFact
	for _, read := range reads {
		evidence = append(evidence, matchCatalog(
			sourceRepoID,
			read.Resource,
			filePath,
			EvidenceKindTerraformIAMPermission,
			RelReadsConfigFrom,
			0.92,
			"Terraform IAM policy grants SSM read access to the target repository config path",
			"terraform-iam-permission",
			catalog,
			seen,
			map[string]any{
				"permission_family": "ssm_config_read",
				"iam_actions":       append([]string(nil), read.Actions...),
				"matched_resource":  read.Resource,
			},
		)...)
	}
	return evidence
}
