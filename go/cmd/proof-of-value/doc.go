// Command proof-of-value measures whether an agent answers IaC reachability
// questions more accurately with Eshu than with plain text search ("grep").
//
// It loads the dead-IaC product-truth fixture corpus
// (tests/fixtures/product_truth/dead_iac) and its curated ground truth
// (tests/fixtures/product_truth/expected/dead_iac.json), then for every
// asserted artifact it runs two real strategies over the same files on disk:
//
//   - baseline_grep: a faithful text-search agent that calls an artifact used
//     when its name appears elsewhere in the corpus and unused otherwise;
//   - eshu: the real internal/iacreachability analyzer.
//
// It scores both strategies against ground truth with internal/proofofvalue
// and prints the with-vs-without delta. With --out it also writes the JSON
// evidence artifact. Every number is computed from real tool output over the
// real corpus; the command fabricates nothing.
package main
