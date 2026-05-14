package mcp

import (
	"encoding/json"

	"github.com/eshu-hq/eshu/go/internal/query"
)

type dispatchResult struct {
	Value    any
	Envelope *query.ResponseEnvelope
	IsError  bool
}

func parseCanonicalEnvelope(body []byte) (*query.ResponseEnvelope, bool) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, false
	}
	if _, ok := top["data"]; !ok {
		return nil, false
	}
	if _, ok := top["truth"]; !ok {
		return nil, false
	}
	if _, ok := top["error"]; !ok {
		return nil, false
	}

	var envelope query.ResponseEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false
	}
	return &envelope, true
}
