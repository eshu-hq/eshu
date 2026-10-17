// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/token"
	"net/http"
	"strconv"
)

// queryIntLiteral returns the value of a decimal int literal.
func queryIntLiteral(lit *ast.BasicLit) (int, bool) {
	if lit.Kind != token.INT {
		return 0, false
	}
	value, err := strconv.Atoi(lit.Value)
	return value, err == nil
}

// queryHTTPStatusValues maps every net/http status constant name to its
// value, so the guards read the real constants rather than a name list.
var queryHTTPStatusValues = map[string]int{
	"StatusAccepted":                      http.StatusAccepted,
	"StatusAlreadyReported":               http.StatusAlreadyReported,
	"StatusBadGateway":                    http.StatusBadGateway,
	"StatusBadRequest":                    http.StatusBadRequest,
	"StatusConflict":                      http.StatusConflict,
	"StatusContinue":                      http.StatusContinue,
	"StatusCreated":                       http.StatusCreated,
	"StatusEarlyHints":                    http.StatusEarlyHints,
	"StatusExpectationFailed":             http.StatusExpectationFailed,
	"StatusFailedDependency":              http.StatusFailedDependency,
	"StatusForbidden":                     http.StatusForbidden,
	"StatusFound":                         http.StatusFound,
	"StatusGatewayTimeout":                http.StatusGatewayTimeout,
	"StatusGone":                          http.StatusGone,
	"StatusHTTPVersionNotSupported":       http.StatusHTTPVersionNotSupported,
	"StatusIMUsed":                        http.StatusIMUsed,
	"StatusInsufficientStorage":           http.StatusInsufficientStorage,
	"StatusInternalServerError":           http.StatusInternalServerError,
	"StatusLengthRequired":                http.StatusLengthRequired,
	"StatusLocked":                        http.StatusLocked,
	"StatusLoopDetected":                  http.StatusLoopDetected,
	"StatusMethodNotAllowed":              http.StatusMethodNotAllowed,
	"StatusMisdirectedRequest":            http.StatusMisdirectedRequest,
	"StatusMovedPermanently":              http.StatusMovedPermanently,
	"StatusMultiStatus":                   http.StatusMultiStatus,
	"StatusMultipleChoices":               http.StatusMultipleChoices,
	"StatusNetworkAuthenticationRequired": http.StatusNetworkAuthenticationRequired,
	"StatusNoContent":                     http.StatusNoContent,
	"StatusNonAuthoritativeInfo":          http.StatusNonAuthoritativeInfo,
	"StatusNotAcceptable":                 http.StatusNotAcceptable,
	"StatusNotExtended":                   http.StatusNotExtended,
	"StatusNotFound":                      http.StatusNotFound,
	"StatusNotImplemented":                http.StatusNotImplemented,
	"StatusNotModified":                   http.StatusNotModified,
	"StatusOK":                            http.StatusOK,
	"StatusPartialContent":                http.StatusPartialContent,
	"StatusPaymentRequired":               http.StatusPaymentRequired,
	"StatusPermanentRedirect":             http.StatusPermanentRedirect,
	"StatusPreconditionFailed":            http.StatusPreconditionFailed,
	"StatusPreconditionRequired":          http.StatusPreconditionRequired,
	"StatusProcessing":                    http.StatusProcessing,
	"StatusProxyAuthRequired":             http.StatusProxyAuthRequired,
	"StatusRequestEntityTooLarge":         http.StatusRequestEntityTooLarge,
	"StatusRequestHeaderFieldsTooLarge":   http.StatusRequestHeaderFieldsTooLarge,
	"StatusRequestTimeout":                http.StatusRequestTimeout,
	"StatusRequestURITooLong":             http.StatusRequestURITooLong,
	"StatusRequestedRangeNotSatisfiable":  http.StatusRequestedRangeNotSatisfiable,
	"StatusResetContent":                  http.StatusResetContent,
	"StatusSeeOther":                      http.StatusSeeOther,
	"StatusServiceUnavailable":            http.StatusServiceUnavailable,
	"StatusSwitchingProtocols":            http.StatusSwitchingProtocols,
	"StatusTeapot":                        http.StatusTeapot,
	"StatusTemporaryRedirect":             http.StatusTemporaryRedirect,
	"StatusTooEarly":                      http.StatusTooEarly,
	"StatusTooManyRequests":               http.StatusTooManyRequests,
	"StatusUnauthorized":                  http.StatusUnauthorized,
	"StatusUnavailableForLegalReasons":    http.StatusUnavailableForLegalReasons,
	"StatusUnprocessableEntity":           http.StatusUnprocessableEntity,
	"StatusUnsupportedMediaType":          http.StatusUnsupportedMediaType,
	"StatusUpgradeRequired":               http.StatusUpgradeRequired,
	"StatusUseProxy":                      http.StatusUseProxy,
	"StatusVariantAlsoNegotiates":         http.StatusVariantAlsoNegotiates,
}
