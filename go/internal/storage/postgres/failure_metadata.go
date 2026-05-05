package postgres

import "errors"

type classifiedFailure interface {
	FailureClass() string
}

type detailedFailure interface {
	FailureDetails() string
}

func queueFailureMetadata(cause error, fallbackClass string) (string, string, string) {
	message := sanitizeFailureText(cause.Error())
	details := message
	failureClass := fallbackClass

	var classified classifiedFailure
	if errors.As(cause, &classified) {
		if class := sanitizeFailureText(classified.FailureClass()); class != "" {
			failureClass = class
		}
	}

	var detailed detailedFailure
	if errors.As(cause, &detailed) {
		if detail := sanitizeFailureText(detailed.FailureDetails()); detail != "" {
			details = detail
		}
	}

	return failureClass, message, details
}
