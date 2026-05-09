// Package confluence collects read-only Confluence page evidence into
// source-neutral documentation facts.
package confluence

// Space is the Confluence space metadata needed for documentation source facts.
type Space struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Name  string `json:"name"`
	Links Links  `json:"_links"`
}

// Page is the Confluence page metadata needed for documentation facts.
type Page struct {
	ID       string       `json:"id"`
	Status   string       `json:"status"`
	Title    string       `json:"title"`
	SpaceID  string       `json:"spaceId"`
	ParentID string       `json:"parentId"`
	OwnerID  string       `json:"ownerId"`
	AuthorID string       `json:"authorId"`
	Version  PageVersion  `json:"version"`
	Body     PageBody     `json:"body"`
	Labels   []Label      `json:"-"`
	LabelSet labelResults `json:"labels"`
	Links    Links        `json:"_links"`
}

// PageVersion is the Confluence page version object.
type PageVersion struct {
	Number    int    `json:"number"`
	CreatedAt string `json:"createdAt"`
	AuthorID  string `json:"authorId"`
}

// PageBody contains supported Confluence page body representations.
type PageBody struct {
	Storage BodyRepresentation `json:"storage"`
}

// BodyRepresentation is one Confluence page body representation.
type BodyRepresentation struct {
	Value          string `json:"value"`
	Representation string `json:"representation"`
}

// Label is one Confluence label.
type Label struct {
	Name string `json:"name"`
}

// Links contains Confluence response links.
type Links struct {
	Base  string `json:"base"`
	WebUI string `json:"webui"`
	Next  string `json:"next"`
}

type pageListResponse struct {
	Results []Page `json:"results"`
	Links   Links  `json:"_links"`
}

type labelResults struct {
	Results []Label `json:"results"`
}
