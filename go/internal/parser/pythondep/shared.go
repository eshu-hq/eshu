package pythondep

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// PackageManager is the canonical package-manager identifier emitted on every
// row produced by this package. It matches what the supply-chain reducer
// normalizes through packageidentity.NormalizeEcosystem.
const PackageManager = "pypi"

// configKindDependency is the entity_metadata.config_kind value the
// supply-chain reducer requires before admitting a manifest dependency as
// registry consumption. Non-registry provenance (path/VCS/URL/editable/
// malformed) MUST use one of the *_dependency siblings below so the reducer
// cannot mis-admit them.
const (
	configKindDependency = "dependency"
	configKindVCS        = "vcs_dependency"
	configKindPath       = "path_dependency"
	configKindURL        = "url_dependency"
	configKindEditable   = "editable_dependency"
	configKindMalformed  = "malformed_dependency"
)

func basePayload(path string, lang string) map[string]any {
	payload := shared.BasePayload(path, lang, false)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}
	return payload
}

// rowBuilder is the shared shape one dependency row carries before being
// flushed into the payload variables bucket. The fields mirror the existing
// npm/composer rows so the supply-chain reducer joins through the same
// content_entity metadata.
type rowBuilder struct {
	Name           string
	LineNumber     int
	Value          string
	Section        string
	ConfigKind     string
	PackageManager string
	Lang           string
	Lockfile       bool
	DevDependency  bool
	Extras         []string
	Marker         string
	SourceKind     string
	SourceURL      string
	SourceRef      string
	Malformed      bool
	Raw            string
}

func (b rowBuilder) finish() map[string]any {
	row := map[string]any{
		"name":            strings.TrimSpace(b.Name),
		"line_number":     b.LineNumber,
		"value":           b.Value,
		"section":         b.Section,
		"config_kind":     b.ConfigKind,
		"package_manager": b.PackageManager,
		"lang":            b.Lang,
	}
	if b.Lockfile {
		row["lockfile"] = true
	}
	if b.DevDependency {
		row["dev_dependency"] = true
	}
	if len(b.Extras) > 0 {
		row["extras"] = append([]string(nil), b.Extras...)
	}
	if b.Marker != "" {
		row["marker"] = b.Marker
	}
	if b.SourceKind != "" {
		row["source_kind"] = b.SourceKind
	}
	if b.SourceURL != "" {
		row["source_url"] = b.SourceURL
	}
	if b.SourceRef != "" {
		row["source_ref"] = b.SourceRef
	}
	if b.Malformed {
		row["malformed"] = true
	}
	if b.Raw != "" {
		row["raw"] = b.Raw
	}
	return row
}
