package query

func buildDeadCodeIncomingBatchProbeCypher(label string) string {
	if !isDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	return `
		UNWIND $entity_ids AS entity_id
		MATCH (e:` + label + ` {uid: entity_id})<-[:CALLS|IMPORTS|REFERENCES|INHERITS|IMPLEMENTS|INSTANTIATES|EXECUTES]-(source)
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id
	`
}
