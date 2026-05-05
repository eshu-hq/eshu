// Package model defines the candidate, evidence, and rejection-reason types
// shared across the correlation pipeline.
//
// All correlation sub-packages (rules, engine, admission, explain) depend on
// these types. Validators here enforce the minimum identity contract a
// candidate or evidence atom must satisfy before downstream evaluation;
// callers must not bypass `Validate` when constructing fixtures.
package model
