// Package guardduty maps Amazon GuardDuty metadata into AWS cloud collector
// facts.
//
// The package owns scanner-level fact selection for detectors, member
// accounts, filter names, publishing destinations, threat intel set metadata,
// and IP set metadata. It emits reported evidence only and keeps finding
// bodies, filter criteria, and threat intel or IP list contents outside the
// scanner contract.
package guardduty
