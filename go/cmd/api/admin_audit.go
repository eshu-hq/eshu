package main

import "github.com/eshu-hq/eshu/go/internal/query"

// adminRecoveryAuditAppender adapts the governance audit reader used elsewhere
// in wiring into the append surface the admin recovery handler needs. The
// concrete governance audit store implements both; a reader that does not
// support appends yields nil, which disables recovery-action audit recording
// without failing startup.
func adminRecoveryAuditAppender(reader query.GovernanceAuditSummaryReader) query.GovernanceAuditAppender {
	if appender, ok := reader.(query.GovernanceAuditAppender); ok {
		return appender
	}
	return nil
}
