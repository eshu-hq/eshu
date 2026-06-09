package query

// EvidenceCitationHandle is the exported name for the canonical
// evidence_citation handle wire shape. It lets sibling packages compose derived
// AnswerPacket views without redefining or weakening the evidence handle
// contract used by the evidence-citation endpoint.
type EvidenceCitationHandle = evidenceCitationHandle
