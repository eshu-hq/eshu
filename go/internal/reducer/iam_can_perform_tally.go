package reducer

func (t *iamCanPerformTally) recordSkip(reason string) {
	switch reason {
	case iamCanPerformSkipUncatalogued:
		t.skippedUncatalogued++
	case iamCanPerformSkipAmbiguous:
		t.skippedAmbiguous++
	case iamCanPerformSkipUnresolved:
		t.skippedUnresolved++
	case iamCanPerformSkipDeny:
		t.skippedDeny++
	case iamCanPerformSkipConditioned:
		t.skippedConditioned++
	case iamCanPerformSkipNotActionResource:
		t.skippedNotActionResource++
	case iamCanPerformSkipSelfLoop:
		t.skippedSelfLoop++
	case iamCanPerformSkipPermissionBoundary:
		t.skippedPermissionBoundary++
	}
}
