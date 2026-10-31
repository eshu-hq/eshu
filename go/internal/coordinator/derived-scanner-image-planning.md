# Derived scanner image planning

`ScannerImagePlanner` is the `scannerworker` child planner interface for
derived scanner-image targets. It groups a bounded protected observation
page by boundary-local manifest. The root reads one page of at most 256
candidates per opted-in instance and reconcile pass, then admits targets
and advances a versioned cursor in one transaction. Static targets remain
on their configured planner path. Logs contain page counts and cursor
version, never source locators.
