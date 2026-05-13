// Package awsruntime adapts AWS cloud service scanners to workflow-claimed
// collector execution.
//
// The package owns claim parsing, target authorization, claim-scoped
// credential acquisition, and collected-generation construction for AWS cloud
// work items. Service scanners own AWS source observation, and reducers own
// canonical graph truth.
package awsruntime
