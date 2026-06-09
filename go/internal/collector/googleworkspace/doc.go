// Package googleworkspace builds source-neutral documentation facts from a
// mocked Google Workspace client boundary.
//
// The package is default-off and contains no live Drive HTTP client, runtime
// flag, chart option, or repository discovery wiring. Callers provide explicit
// file, folder, or shared-drive allowlists and a mocked Client; Collect maps
// Docs, Sheets, and Slides exports into documentation source, document, section,
// and link facts without inferring graph, service, deployment, incident, or
// ownership truth.
package googleworkspace
