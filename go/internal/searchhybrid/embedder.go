package searchhybrid

// Embedder produces a fixed-dimension local embedding for a piece of text.
//
// Implementations MUST be deterministic for a given input and MUST NOT call a
// hosted service: the design-430 default search path has no hosted LLM
// dependency. The concrete local model (for example a small arctic-embed-class
// model) is supplied by a caller; this package only depends on the port so BM25
// retrieval works with no embedder at all.
type Embedder interface {
	// Embed returns the embedding vector for text. The returned slice length
	// must equal Dimensions for every call.
	Embed(text string) ([]float64, error)
	// Dimensions is the fixed vector length produced by Embed.
	Dimensions() int
}
