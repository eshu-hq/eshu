package reducer

import (
	"strings"
)

// TerraformRuntimeFamily describes one Terraform-managed runtime family.
type TerraformRuntimeFamily struct {
	Kind                     string
	Provider                 string
	DisplayName              string
	NameHints                []string
	ClusterModulePatterns    []string
	ClusterResourceTypes     []string
	ServiceModulePatterns    []string
	NonClusterModulePatterns []string
}

var runtimeFamilies = []TerraformRuntimeFamily{
	{
		Kind:                     "ecs",
		Provider:                 "aws",
		DisplayName:              "ECS",
		NameHints:                []string{"ecs", "fargate"},
		ClusterModulePatterns:    []string{"batch-compute-resource/aws", "ecs-cluster/aws"},
		ClusterResourceTypes:     []string{"aws_ecs_cluster"},
		ServiceModulePatterns:    []string{"ecs-application/aws"},
		NonClusterModulePatterns: []string{"ecs-application/aws"},
	},
	{
		Kind:                     "eks",
		Provider:                 "aws",
		DisplayName:              "EKS",
		NameHints:                []string{"eks"},
		ClusterModulePatterns:    []string{"terraform-aws-modules/eks/aws", "eks-blueprints", "eks-cluster"},
		ClusterResourceTypes:     []string{"aws_eks_cluster"},
		ServiceModulePatterns:    nil,
		NonClusterModulePatterns: []string{"iam-role-for-service-accounts-eks"},
	},
	{
		Kind:                     "lambda",
		Provider:                 "aws",
		DisplayName:              "Lambda",
		NameHints:                []string{"lambda", "serverless"},
		ClusterModulePatterns:    nil,
		ClusterResourceTypes:     []string{"aws_lambda_function"},
		ServiceModulePatterns:    []string{"lambda-function", "serverless-function"},
		NonClusterModulePatterns: nil,
	},
	{
		Kind:                     "cloudflare_workers",
		Provider:                 "cloudflare",
		DisplayName:              "Cloudflare Workers",
		NameHints:                []string{"cloudflare", "workers"},
		ClusterModulePatterns:    nil,
		ClusterResourceTypes:     []string{"cloudflare_workers_script"},
		ServiceModulePatterns:    []string{"cloudflare-worker"},
		NonClusterModulePatterns: nil,
	},
	{
		Kind:                     "gke",
		Provider:                 "gcp",
		DisplayName:              "GKE",
		NameHints:                []string{"gke"},
		ClusterModulePatterns:    []string{"terraform-google-modules/kubernetes-engine", "gke-cluster"},
		ClusterResourceTypes:     []string{"google_container_cluster"},
		ServiceModulePatterns:    nil,
		NonClusterModulePatterns: nil,
	},
	{
		Kind:                     "aks",
		Provider:                 "azure",
		DisplayName:              "AKS",
		NameHints:                []string{"aks"},
		ClusterModulePatterns:    []string{"Azure/aks/azurerm", "aks-cluster"},
		ClusterResourceTypes:     []string{"azurerm_kubernetes_cluster"},
		ServiceModulePatterns:    nil,
		NonClusterModulePatterns: nil,
	},
	{
		Kind:        "cloud_run",
		Provider:    "gcp",
		DisplayName: "Cloud Run",
		NameHints:   []string{"cloud-run", "cloud_run", "cloudrun"},
		ClusterResourceTypes: []string{
			"google_cloud_run_service",
			"google_cloud_run_v2_service",
		},
		ServiceModulePatterns: []string{"cloud-run"},
	},
	{
		Kind:                 "container_apps",
		Provider:             "azure",
		DisplayName:          "Azure Container Apps",
		NameHints:            []string{"container-apps", "container_apps"},
		ClusterResourceTypes: []string{"azurerm_container_app"},
	},
}

// RuntimeFamilies returns all registered Terraform runtime families.
func RuntimeFamilies() []TerraformRuntimeFamily {
	result := make([]TerraformRuntimeFamily, len(runtimeFamilies))
	copy(result, runtimeFamilies)
	return result
}

// LookupRuntimeFamily returns one registered runtime family by normalized kind.
func LookupRuntimeFamily(kind string) *TerraformRuntimeFamily {
	normalized := strings.TrimSpace(strings.ToLower(kind))
	for i := range runtimeFamilies {
		if runtimeFamilies[i].Kind == normalized {
			return &runtimeFamilies[i]
		}
	}
	return nil
}

// InferTerraformRuntimeFamilyKind infers the runtime family kind from
// Terraform content by matching resource types and module patterns.
func InferTerraformRuntimeFamilyKind(content string) string {
	lower := strings.ToLower(content)
	for _, f := range runtimeFamilies {
		for _, rt := range f.ClusterResourceTypes {
			if strings.Contains(lower, rt) {
				return f.Kind
			}
		}
		for _, pattern := range f.ClusterModulePatterns {
			if strings.Contains(lower, pattern) {
				return f.Kind
			}
		}
	}
	return ""
}

// InferRuntimeFamilyKindFromIdentifiers infers a runtime family kind from
// repo names, slugs, or other identifiers.
func InferRuntimeFamilyKindFromIdentifiers(values []string) string {
	var normalized []string
	for _, v := range values {
		s := strings.TrimSpace(strings.ToLower(v))
		if s != "" {
			normalized = append(normalized, s)
		}
	}
	for _, f := range runtimeFamilies {
		for _, nv := range normalized {
			for _, hint := range f.NameHints {
				if strings.Contains(nv, hint) {
					return f.Kind
				}
			}
		}
	}
	return ""
}

// InferInfrastructureRuntimeFamilyKind infers a runtime family for infra repos
// with explicit cluster signals, excluding families that match non-cluster
// module patterns.
func InferInfrastructureRuntimeFamilyKind(resourceTypes, moduleSources []string) string {
	normalizedRT := normalizeSet(resourceTypes)
	normalizedMS := normalizeSet(moduleSources)

	for _, f := range runtimeFamilies {
		hasCluster := false
		hasExplicitClusterResource := false
		for _, rt := range f.ClusterResourceTypes {
			if _, ok := normalizedRT[rt]; ok {
				hasCluster = true
				hasExplicitClusterResource = true
				break
			}
		}
		if !hasCluster {
			for _, pattern := range f.ClusterModulePatterns {
				for ms := range normalizedMS {
					if strings.Contains(ms, pattern) {
						hasCluster = true
						break
					}
				}
				if hasCluster {
					break
				}
			}
		}
		if !hasCluster {
			continue
		}

		excluded := false
		excludedByServiceModule := false
		for _, pattern := range f.NonClusterModulePatterns {
			for ms := range normalizedMS {
				if strings.Contains(ms, pattern) {
					excluded = true
					excludedByServiceModule = matchesAnyPattern(pattern, f.ServiceModulePatterns)
					break
				}
			}
			if excluded {
				break
			}
		}
		// Service-only modules should not turn an application stack into an
		// infrastructure platform, but an explicit cluster resource is stronger
		// evidence than sibling service modules in the same stack.
		if excluded && (!hasExplicitClusterResource || !excludedByServiceModule) {
			continue
		}
		return f.Kind
	}
	return ""
}

// matchesAnyPattern reports whether value and a registered pattern describe
// the same module family after normalization by the caller.
func matchesAnyPattern(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(value, pattern) || strings.Contains(pattern, value) {
			return true
		}
	}
	return false
}

// MatchesServiceModuleSource reports whether one module source matches the
// registered service patterns for the given runtime family kind.
func MatchesServiceModuleSource(source, kind string) bool {
	f := LookupRuntimeFamily(kind)
	if f == nil || len(f.ServiceModulePatterns) == 0 {
		return false
	}
	normalized := strings.TrimSpace(strings.ToLower(source))
	for _, pattern := range f.ServiceModulePatterns {
		if strings.Contains(normalized, pattern) {
			return true
		}
	}
	return false
}

// TerraformPlatformEvidenceKind builds a stable Terraform evidence kind for
// one runtime family and scope.
func TerraformPlatformEvidenceKind(kind, scope string) string {
	nk := strings.TrimSpace(strings.ToUpper(kind))
	if nk == "" {
		nk = "UNKNOWN"
	}
	ns := strings.TrimSpace(strings.ToUpper(scope))
	if ns == "" {
		ns = "UNKNOWN"
	}
	return "TERRAFORM_" + nk + "_" + ns
}

// FormatPlatformKindLabel returns a human-readable label for one platform kind.
func FormatPlatformKindLabel(kind string) string {
	normalized := strings.TrimSpace(strings.ToLower(kind))
	if normalized == "" {
		return ""
	}
	f := LookupRuntimeFamily(normalized)
	if f != nil {
		return f.DisplayName
	}
	if normalized == "kubernetes" {
		return "Kubernetes"
	}
	return strings.ToUpper(normalized)
}

func normalizeSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, v := range values {
		s := strings.TrimSpace(strings.ToLower(v))
		if s != "" {
			result[s] = struct{}{}
		}
	}
	return result
}
