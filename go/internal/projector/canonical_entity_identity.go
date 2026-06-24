// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package projector

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
)

var canonicalNamePathLineEntityLabels = map[string]struct{}{
	"Annotation":              {},
	"ArgoCDApplication":       {},
	"ArgoCDApplicationSet":    {},
	"Class":                   {},
	"CloudFormationOutput":    {},
	"CloudFormationParameter": {},
	"CloudFormationResource":  {},
	"CrossplaneComposition":   {},
	"CrossplaneXRD":           {},
	"Enum":                    {},
	"Function":                {},
	"Interface":               {},
	"Macro":                   {},
	"Property":                {},
	"Record":                  {},
	"Struct":                  {},
	"TerraformBackend":        {},
	"TerraformCheck":          {},
	"TerraformDataSource":     {},
	"TerraformImport":         {},
	"TerraformLocal":          {},
	"TerraformLockProvider":   {},
	"TerraformMovedBlock":     {},
	"TerraformOutput":         {},
	"TerraformProvider":       {},
	"TerraformRemovedBlock":   {},
	"TerraformResource":       {},
	"TerraformVariable":       {},
	"TerragruntDependency":    {},
	"TerragruntInput":         {},
	"TerragruntLocal":         {},
	"Trait":                   {},
	"TypeAnnotation":          {},
	"Union":                   {},
	"Variable":                {},
}

func canonicalGraphEntityID(
	label string,
	repoID string,
	relativePath string,
	entityType string,
	entityName string,
	startLine int,
	incomingID string,
) string {
	if _, ok := canonicalNamePathLineEntityLabels[label]; ok {
		return content.CanonicalEntityID(repoID, relativePath, entityType, entityName, startLine)
	}
	if strings.TrimSpace(incomingID) != "" {
		return incomingID
	}
	return content.CanonicalEntityID(repoID, relativePath, entityType, entityName, startLine)
}

func canonicalEntityDedupKey(entity EntityRow) string {
	return entity.Label + "\x00" + entity.EntityID
}
