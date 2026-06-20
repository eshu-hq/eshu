// Package searchembedruntime selects the semantic-search embedder used by
// Eshu runtimes.
//
// The package keeps API, MCP, and reducer wiring on one profile-selection
// contract. It preserves the explicit local hash fallback, enables a single
// governed search_documents provider profile by default after policy/egress
// admission, and fails closed when multiple provider profiles need an explicit
// selector.
package searchembedruntime
