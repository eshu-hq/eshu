// Command search-bench runs the design-430 search-lane benchmark over a live
// Eshu content corpus.
//
// It loads content entities and files for one repository from the Postgres
// content store, projects them into curated search documents with the shared
// searchdocs projection, and measures keyword-retrieval latency for the current
// Postgres content-search baseline against the in-process curated hybrid lane
// (internal/searchhybrid) over that real corpus.
//
// The benchmark reports real measured latency, index build cost, and curated
// corpus shape. It does not fabricate the NornicDB search-lane arm: the
// canonical NornicDB runs search-disabled per design 430, and no search-enabled
// curated NornicDB deployment exists yet. Recall and precision require a labeled
// query suite and are out of scope for this latency-and-cost run.
package main
