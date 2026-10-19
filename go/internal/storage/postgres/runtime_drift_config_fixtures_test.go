// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

// Config-parser fixture builders shared by the AWS and multi-cloud
// runtime-drift evidence tests. They mirror the parser's
// parsed_file_data.terraform_resources row shape from
// go/internal/parser/hcl/parser.go:130-154: the loader reconstructs the
// canonical address from resource_type + resource_name (the parser does not
// emit a single "address" key on the config side).
//
// These are copies of the tfstate family's builders
// (tfstate/drift_evidence_test.go): the tfstate move (issue #8361)
// took the originals into package tfstate, and test files cannot be
// imported across packages. Keep the two copies identical; the AWS family
// move will retire this copy when its tests follow tfstate.

// fixtureConfigParserRow builds one parser-shaped terraform_resources row.
func fixtureConfigParserRow(resourceType, resourceName string) string {
	return `{
		"name":"` + resourceType + `.` + resourceName + `",
		"resource_type":"` + resourceType + `",
		"resource_name":"` + resourceName + `",
		"path":"main.tf",
		"lang":"hcl",
		"line_number":1
	}`
}

// fixtureConfigResourcesArray wraps one or more parser rows into the JSON
// array shape stored at parsed_file_data.terraform_resources.
func fixtureConfigResourcesArray(rows ...string) []byte {
	out := "["
	for i, row := range rows {
		if i > 0 {
			out += ","
		}
		out += row
	}
	return []byte(out + "]")
}
