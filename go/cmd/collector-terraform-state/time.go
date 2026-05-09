package main

import "time"

func timeZeroUTC() time.Time {
	return time.Time{}.UTC()
}
