package doctruth

import (
	"regexp"
	"strings"
)

var terraformAddressPattern = regexp.MustCompile(`^(?:terraform/)?(?:(?:data\.)?[a-z][a-z0-9_]*\.[A-Za-z0-9_-]+|module\.[A-Za-z0-9_-]+)$`)

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
	return strings.TrimPrefix(value, "terraform/")
}
