package reducer

func nugetSemverRangeContainsDecision(
	affectedRange supplyChainAffectedRange,
	observed string,
) (bool, bool) {
	if ok, valid := nugetVersionBeforeLimitsDecision(observed, affectedRange.events); !valid {
		return false, false
	} else if !ok {
		return false, true
	}
	vulnerable := false
	for _, event := range affectedRange.events {
		switch {
		case event.introduced != "":
			if ok, valid := nugetVersionAtLeast(observed, event.introduced); !valid {
				return false, false
			} else if ok {
				vulnerable = true
			}
		case event.fixed != "":
			if ok, valid := nugetVersionAtLeast(observed, event.fixed); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		case event.lastAffected != "":
			if ok, valid := nugetVersionGreaterThan(observed, event.lastAffected); !valid {
				return false, false
			} else if ok {
				vulnerable = false
			}
		}
	}
	return vulnerable, true
}

func nugetVersionBeforeLimitsDecision(observed string, events []supplyChainAffectedRangeEvent) (bool, bool) {
	hasLimit := false
	for _, event := range events {
		limit := event.limit
		if limit == "" {
			continue
		}
		hasLimit = true
		if limit == "*" {
			return true, true
		}
		if ok, valid := nugetVersionLessThan(observed, limit); !valid {
			return false, false
		} else if ok {
			return true, true
		}
	}
	return !hasLimit, true
}

func nugetVersionAtLeast(left string, right string) (bool, bool) {
	cmp, ok := compareNuGetVersion(left, right)
	return cmp >= 0, ok
}

func nugetVersionGreaterThan(left string, right string) (bool, bool) {
	cmp, ok := compareNuGetVersion(left, right)
	return cmp > 0, ok
}

func nugetVersionLessThan(left string, right string) (bool, bool) {
	cmp, ok := compareNuGetVersion(left, right)
	return cmp < 0, ok
}
