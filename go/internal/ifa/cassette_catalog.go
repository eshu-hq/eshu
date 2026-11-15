// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"errors"

	"github.com/eshu-hq/eshu/go/internal/ifa/familyodu"
)

// The Odù names the vulnerability collector's recording backs
// (eshu-hq/eshu#10734). They exist only in the cassette-backed catalog,
// CatalogAt and CatalogByNameAt.
const (
	// VulnCollectorOduName is the NVD product-path Odù: one recorded window, KEV
	// and EPSS, and a planted SBOM component with the exact CPE of the window's
	// first record.
	VulnCollectorOduName = "odu:vuln-collector-nvd"
	// VulnCollectorWildcardRangeOduName plants a component the window's wildcard
	// CPE only matches through its version range, which the exact compare misses.
	VulnCollectorWildcardRangeOduName = "odu:vuln-collector-nvd-wildcard-range"
	// VulnCollectorRejectedOduName plants a component for a rejected CVE.
	VulnCollectorRejectedOduName = "odu:vuln-collector-nvd-rejected"
	// VulnCollectorNewestCopyOduName selects the window that re-issues a CVE as
	// rejected, beside the window that first carried it.
	VulnCollectorNewestCopyOduName = "odu:vuln-collector-nvd-newest-copy"
	// VulnCollectorNoBaselineOduName plants a component for a CVE only an older
	// window carries.
	VulnCollectorNoBaselineOduName = "odu:vuln-collector-nvd-no-baseline"
)

// CatalogAt is not implemented yet.
func CatalogAt(repoRoot string) ([]familyodu.CatalogOdu, error) {
	return nil, errors.New("ifa: cassette-backed catalog entries are not implemented")
}

// CatalogByNameAt is not implemented yet.
func CatalogByNameAt(repoRoot string) (map[string]familyodu.Odu, error) {
	return nil, errors.New("ifa: cassette-backed catalog entries are not implemented")
}
