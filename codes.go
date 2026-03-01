package trpcgo

import "net/http"

// ErrorCode represents JSON-RPC 2.0 error codes used by the tRPC wire protocol.
type ErrorCode int

const (
	CodeParseError          ErrorCode = -32700
	CodeBadRequest          ErrorCode = -32600
	CodeInternalServerError ErrorCode = -32603
	CodeUnauthorized        ErrorCode = -32001
	CodeForbidden           ErrorCode = -32003
	CodeNotFound            ErrorCode = -32004
	CodeMethodNotSupported  ErrorCode = -32005
	CodeTimeout             ErrorCode = -32008
	CodeConflict            ErrorCode = -32009
	CodePreconditionFailed  ErrorCode = -32012
	CodePayloadTooLarge     ErrorCode = -32013
	CodeUnsupportedMedia    ErrorCode = -32015
	CodeUnprocessableContent ErrorCode = -32022
	CodePreconditionRequired ErrorCode = -32028
	CodeTooManyRequests     ErrorCode = -32029
	CodeClientClosed        ErrorCode = -32099
	CodeNotImplemented      ErrorCode = -32501
	CodeBadGateway          ErrorCode = -32502
	CodeServiceUnavailable  ErrorCode = -32503
	CodeGatewayTimeout      ErrorCode = -32504
)

var codeToHTTPStatus = map[ErrorCode]int{
	CodeParseError:           http.StatusBadRequest,
	CodeBadRequest:           http.StatusBadRequest,
	CodeUnauthorized:         http.StatusUnauthorized,
	CodeForbidden:            http.StatusForbidden,
	CodeNotFound:             http.StatusNotFound,
	CodeMethodNotSupported:   http.StatusMethodNotAllowed,
	CodeTimeout:              http.StatusRequestTimeout,
	CodeConflict:             http.StatusConflict,
	CodePreconditionFailed:   http.StatusPreconditionFailed,
	CodePayloadTooLarge:      http.StatusRequestEntityTooLarge,
	CodeUnsupportedMedia:     http.StatusUnsupportedMediaType,
	CodeUnprocessableContent: http.StatusUnprocessableEntity,
	CodePreconditionRequired: http.StatusPreconditionRequired,
	CodeTooManyRequests:      http.StatusTooManyRequests,
	CodeClientClosed:         499,
	CodeInternalServerError:  http.StatusInternalServerError,
	CodeNotImplemented:       http.StatusNotImplemented,
	CodeBadGateway:           http.StatusBadGateway,
	CodeServiceUnavailable:   http.StatusServiceUnavailable,
	CodeGatewayTimeout:       http.StatusGatewayTimeout,
}

var codeToName = map[ErrorCode]string{
	CodeParseError:           "PARSE_ERROR",
	CodeBadRequest:           "BAD_REQUEST",
	CodeUnauthorized:         "UNAUTHORIZED",
	CodeForbidden:            "FORBIDDEN",
	CodeNotFound:             "NOT_FOUND",
	CodeMethodNotSupported:   "METHOD_NOT_SUPPORTED",
	CodeTimeout:              "TIMEOUT",
	CodeConflict:             "CONFLICT",
	CodePreconditionFailed:   "PRECONDITION_FAILED",
	CodePayloadTooLarge:      "PAYLOAD_TOO_LARGE",
	CodeUnsupportedMedia:     "UNSUPPORTED_MEDIA_TYPE",
	CodeUnprocessableContent: "UNPROCESSABLE_CONTENT",
	CodePreconditionRequired: "PRECONDITION_REQUIRED",
	CodeTooManyRequests:      "TOO_MANY_REQUESTS",
	CodeClientClosed:         "CLIENT_CLOSED_REQUEST",
	CodeInternalServerError:  "INTERNAL_SERVER_ERROR",
	CodeNotImplemented:       "NOT_IMPLEMENTED",
	CodeBadGateway:           "BAD_GATEWAY",
	CodeServiceUnavailable:   "SERVICE_UNAVAILABLE",
	CodeGatewayTimeout:       "GATEWAY_TIMEOUT",
}

// HTTPStatusFromCode returns the HTTP status code for a tRPC error code.
func HTTPStatusFromCode(code ErrorCode) int {
	if status, ok := codeToHTTPStatus[code]; ok {
		return status
	}
	return http.StatusInternalServerError
}

// NameFromCode returns the string name for a tRPC error code (e.g. "NOT_FOUND").
func NameFromCode(code ErrorCode) string {
	if name, ok := codeToName[code]; ok {
		return name
	}
	return "INTERNAL_SERVER_ERROR"
}
