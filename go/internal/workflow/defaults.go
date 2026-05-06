package workflow

import "time"

const (
	defaultClaimLeaseTTL            = 60 * time.Second
	defaultHeartbeatInterval        = 20 * time.Second
	defaultReapInterval             = 20 * time.Second
	defaultExpiredClaimLimit        = 100
	defaultExpiredClaimRequeueDelay = 5 * time.Second
)

func DefaultClaimLeaseTTL() time.Duration {
	return defaultClaimLeaseTTL
}

func DefaultHeartbeatInterval() time.Duration {
	return defaultHeartbeatInterval
}

func DefaultReapInterval() time.Duration {
	return defaultReapInterval
}

func DefaultExpiredClaimLimit() int {
	return defaultExpiredClaimLimit
}

func DefaultExpiredClaimRequeueDelay() time.Duration {
	return defaultExpiredClaimRequeueDelay
}
