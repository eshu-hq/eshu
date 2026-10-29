// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

// jvmDefinitions returns the JVM-ecosystem parser definitions: the Java,
// Kotlin, and Scala languages and their Maven, Gradle, and sbt build-file
// bindings. They live here, appended to defaultDefinitions in
// registry_definitions.go, so that file stays under the 500-line cap.
// NewRegistry sorts by parser key and normalizes every binding list, so the
// spread keeps registration semantics identical to inline entries. Add new
// JVM-ecosystem registrations here, not in registry_definitions.go.
func jvmDefinitions() []Definition {
	return []Definition{
		{
			ParserKey:  "java",
			Language:   "java",
			Ecosystem:  "java",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".java"},
		},
		{
			ParserKey: "java_metadata",
			Language:  "java_metadata",
			Ecosystem: "java",
			Admission: AdmissionStructuredFact,
		},
		{
			ParserKey:  "gradle",
			Language:   "gradle",
			Ecosystem:  "java",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"build.gradle", "build.gradle.kts", "libs.versions.toml"},
		},
		{
			// gradle-wrapper.properties pins the Gradle distribution, and its
			// distributionUrl can carry user:password@ userinfo. It is its own
			// exact-name definition, never bound to the gradle definition above,
			// and it binds the metadata content-store policy (#8822): the row
			// keeps the path and the source digest with an empty body, and the
			// wrapper parser emits only the version and the distribution type.
			// Parser output is not governed by the policy, so the parser, not
			// the policy, keeps credentials out of facts.
			ParserKey:    "gradle_wrapper",
			Language:     "gradle_wrapper",
			Ecosystem:    "java",
			Admission:    AdmissionStructuredFact,
			ContentStore: ContentStore{Policy: ContentStoreMetadata},
			ExactNames:   []string{"gradle-wrapper.properties"},
		},
		{
			ParserKey:  "kotlin",
			Language:   "kotlin",
			Ecosystem:  "kotlin",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".kt", ".kts"},
		},
		{
			ParserKey:  "maven",
			Language:   "maven",
			Ecosystem:  "java",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"pom.xml"},
		},
		{
			ParserKey:  "scala",
			Language:   "scala",
			Ecosystem:  "scala",
			Admission:  AdmissionStructuredFact,
			Extensions: []string{".sc", ".scala", ".routes"},
		},
		{
			ParserKey:  "sbt",
			Language:   "sbt",
			Ecosystem:  "scala",
			Admission:  AdmissionStructuredFact,
			ExactNames: []string{"build.sbt"},
		},
		{
			ParserKey:    "sbt_properties",
			Language:     "sbt_properties",
			Ecosystem:    "scala",
			Admission:    AdmissionStructuredFact,
			ContentStore: ContentStore{Policy: ContentStoreMetadata},
			// project/build.properties is path-dispatched by PathDispatches in
			// registry_definitions.go. The base name alone is too generic for
			// a repository-wide admission.
		},
	}
}
