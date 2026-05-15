package status

// TerraformStateReport projects the per-locator serial and recent warning
// evidence into a stable shape for the admin status surface. The Postgres
// query bounds raw inputs; this projection only sorts and groups them.
type TerraformStateReport struct {
	LastSerials    []TerraformStateLocatorSerial
	RecentWarnings []TerraformStateLocatorWarning
	WarningsByKind map[string]map[string][]TerraformStateLocatorWarning
}
