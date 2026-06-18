package collector

import "sort"

// buildDataflowCatalogVersions reads parser-emitted dataflow catalog versions
// from parsed file payloads. The versions are freshness-only metadata: they do
// not stream as facts, but they make catalog-only taint changes re-run the
// value-flow path for unchanged source files.
func buildDataflowCatalogVersions(parsedFiles []map[string]any) []DataflowCatalogVersionSnapshot {
	seen := map[DataflowCatalogVersionSnapshot]bool{}
	for _, parsedFile := range parsedFiles {
		rows, _ := parsedFile["dataflow_catalog_versions"].([]map[string]any)
		for _, row := range rows {
			version := DataflowCatalogVersionSnapshot{
				Language: snapshotPayloadString(row, "lang", "language"),
				Catalog:  snapshotPayloadString(row, "catalog"),
				Version:  snapshotPayloadString(row, "version"),
			}
			if version.Language == "" || version.Catalog == "" || version.Version == "" {
				continue
			}
			seen[version] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	versions := make([]DataflowCatalogVersionSnapshot, 0, len(seen))
	for version := range seen {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool {
		if versions[i].Language != versions[j].Language {
			return versions[i].Language < versions[j].Language
		}
		if versions[i].Catalog != versions[j].Catalog {
			return versions[i].Catalog < versions[j].Catalog
		}
		return versions[i].Version < versions[j].Version
	})
	return versions
}
