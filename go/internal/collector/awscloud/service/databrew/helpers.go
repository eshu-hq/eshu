// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package databrew

import (
	"strings"
	"time"
)

// datasetResourceID returns the resource_id the dataset node publishes. It uses
// the dataset name because jobs and projects reference a dataset by name, so
// keying the dataset node on its name lets those internal edges join exactly.
func datasetResourceID(dataset Dataset) string {
	return strings.TrimSpace(dataset.Name)
}

// recipeResourceID returns the resource_id the recipe node publishes. It uses
// the recipe name because projects reference a recipe by name, so keying the
// recipe node on its name lets the project-uses-recipe edge join exactly.
func recipeResourceID(recipe Recipe) string {
	return strings.TrimSpace(recipe.Name)
}

// jobResourceID returns the resource_id the job node publishes. It prefers the
// job ARN and falls back to the job name.
func jobResourceID(job Job) string {
	return firstNonEmpty(job.ARN, job.Name)
}

// projectResourceID returns the resource_id the project node publishes. It
// prefers the project ARN and falls back to the project name.
func projectResourceID(project Project) string {
	return firstNonEmpty(project.ARN, project.Name)
}

// arnForBucket synthesizes the partition-aware S3 bucket ARN for name, or
// returns an already-formed ARN unchanged. S3 buckets have no API ARN, so the
// scanner synthesizes one carrying the boundary partition (aws / aws-cn /
// aws-us-gov) so the dataset/job -> bucket target matches the S3 scanner's
// published bucket resource_id in every partition instead of dangling the edge.
func arnForBucket(partition, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "arn:") {
		return name
	}
	return "arn:" + partition + ":s3:::" + name
}

// glueTableResourceID returns the "<database>/<table>" identity the Glue table
// scanner publishes as its resource_id, or "" when either part is missing. It
// mirrors the Glue scanner's tableResourceID so a dataset -> Glue table edge
// joins the table node instead of dangling.
func glueTableResourceID(database, table string) string {
	database = strings.TrimSpace(database)
	table = strings.TrimSpace(table)
	if database == "" || table == "" {
		return ""
	}
	return database + "/" + table
}

// isARN reports whether value carries the canonical AWS ARN prefix.
func isARN(value string) bool {
	return strings.HasPrefix(strings.TrimSpace(value), "arn:")
}

// firstNonEmpty returns the first trimmed non-empty value, or "" when none.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// timeOrNil returns the UTC time when value is set, or nil for the zero time so
// the attribute payload omits an unknown timestamp instead of emitting an epoch.
func timeOrNil(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC()
}

// cloneStringMap returns a trimmed-key copy of input, or nil when it is empty or
// every key trims to empty, keeping omitempty-style payload behavior consistent.
func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// cloneStrings returns a trimmed copy of input with empty entries dropped, or
// nil when nothing survives.
func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
