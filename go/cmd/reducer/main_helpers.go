package main

import "github.com/eshu-hq/eshu/go/internal/reducer"

// reducerDomainStrings renders reducer domains as plain strings for structured
// log fields. It lives here rather than in main.go to keep that file within the
// repo file-size budget.
func reducerDomainStrings(domains []reducer.Domain) []string {
	values := make([]string, 0, len(domains))
	for _, domain := range domains {
		values = append(values, string(domain))
	}
	return values
}
