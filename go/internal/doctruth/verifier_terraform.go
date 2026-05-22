package doctruth

import (
	"regexp"
	"strings"
)

var terraformAddressPattern = regexp.MustCompile(`^(?:terraform/)?(?:(?:data\.)?[a-z][a-z0-9]*_[a-z0-9_]*\.[A-Za-z0-9_-]+|module\.[A-Za-z0-9_-]+)$`)

var terraformProviderTypePrefixes = []string{
	"aws_",
	"azurerm_",
	"cloudflare_",
	"datadog_",
	"docker_",
	"github_",
	"google_",
	"helm_",
	"kubernetes_",
	"local_",
	"null_",
	"postgresql_",
	"random_",
	"time_",
	"tls_",
	"vault_",
}

// TerraformAddressResolver checks one normalized Terraform address claim against caller-owned truth.
type TerraformAddressResolver func(DocumentInput, string) TerraformAddressResolution

// TerraformAddressResolution is the caller-supplied truth result for a Terraform address claim.
type TerraformAddressResolution struct {
	Supported bool
	Exists    bool
}

// NormalizeTerraformAddressClaim returns a supported Terraform address claim or an empty string.
func NormalizeTerraformAddressClaim(raw string) string {
	value := strings.TrimSpace(raw)
	value = strings.TrimRight(value, ".,;:")
	if !terraformAddressPattern.MatchString(value) {
		return ""
	}
	value = strings.TrimPrefix(value, "terraform/")
	if strings.HasPrefix(value, "module.") {
		return value
	}
	resourceTypeSource := value
	if strings.HasPrefix(resourceTypeSource, "data.") {
		resourceTypeSource = strings.TrimPrefix(resourceTypeSource, "data.")
	}
	resourceType := resourceTypeSource[:strings.Index(resourceTypeSource, ".")]
	if !looksLikeTerraformProviderResourceType(resourceType) {
		return ""
	}
	return value
}

func looksLikeTerraformProviderResourceType(resourceType string) bool {
	for _, prefix := range terraformProviderTypePrefixes {
		if strings.HasPrefix(resourceType, prefix) {
			return true
		}
	}
	return false
}
