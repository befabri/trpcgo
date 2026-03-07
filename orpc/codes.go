package orpc

import (
	"net/http"

	"github.com/befabri/trpcgo"
)

// Error code strings matching the oRPC protocol.
const (
	CodeBadRequest          = "BAD_REQUEST"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeForbidden           = "FORBIDDEN"
	CodeNotFound            = "NOT_FOUND"
	CodeMethodNotSupported  = "METHOD_NOT_SUPPORTED"
	CodeNotAcceptable       = "NOT_ACCEPTABLE"
	CodeTimeout             = "TIMEOUT"
	CodeConflict            = "CONFLICT"
	CodePreconditionFailed  = "PRECONDITION_FAILED"
	CodePayloadTooLarge     = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMedia    = "UNSUPPORTED_MEDIA_TYPE"
	CodeUnprocessableContent = "UNPROCESSABLE_CONTENT"
	CodeTooManyRequests     = "TOO_MANY_REQUESTS"
	CodeClientClosed        = "CLIENT_CLOSED_REQUEST"
	CodeInternalServerError = "INTERNAL_SERVER_ERROR"
	CodeNotImplemented      = "NOT_IMPLEMENTED"
	CodeBadGateway          = "BAD_GATEWAY"
	CodeServiceUnavailable  = "SERVICE_UNAVAILABLE"
	CodeGatewayTimeout      = "GATEWAY_TIMEOUT"
)

var codeToStatus = map[string]int{
	CodeBadRequest:          http.StatusBadRequest,
	CodeUnauthorized:        http.StatusUnauthorized,
	CodeForbidden:           http.StatusForbidden,
	CodeNotFound:            http.StatusNotFound,
	CodeMethodNotSupported:  http.StatusMethodNotAllowed,
	CodeNotAcceptable:       http.StatusNotAcceptable,
	CodeTimeout:             http.StatusRequestTimeout,
	CodeConflict:            http.StatusConflict,
	CodePreconditionFailed:  http.StatusPreconditionFailed,
	CodePayloadTooLarge:     http.StatusRequestEntityTooLarge,
	CodeUnsupportedMedia:    http.StatusUnsupportedMediaType,
	CodeUnprocessableContent: http.StatusUnprocessableEntity,
	CodeTooManyRequests:     http.StatusTooManyRequests,
	CodeClientClosed:        499,
	CodeInternalServerError: http.StatusInternalServerError,
	CodeNotImplemented:      http.StatusNotImplemented,
	CodeBadGateway:          http.StatusBadGateway,
	CodeServiceUnavailable:  http.StatusServiceUnavailable,
	CodeGatewayTimeout:      http.StatusGatewayTimeout,
}

// StatusFromCode returns the HTTP status for an oRPC error code string.
func StatusFromCode(code string) int {
	if s, ok := codeToStatus[code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// trpcgo uses integer error codes; map them to oRPC string codes.
var trpcCodeToORPC = map[trpcgo.ErrorCode]string{
	trpcgo.CodeParseError:          CodeBadRequest,
	trpcgo.CodeBadRequest:          CodeBadRequest,
	trpcgo.CodeInternalServerError: CodeInternalServerError,
	trpcgo.CodeUnauthorized:        CodeUnauthorized,
	trpcgo.CodeForbidden:           CodeForbidden,
	trpcgo.CodeNotFound:            CodeNotFound,
	trpcgo.CodeMethodNotSupported:  CodeMethodNotSupported,
	trpcgo.CodeTimeout:             CodeTimeout,
	trpcgo.CodeConflict:            CodeConflict,
	trpcgo.CodePreconditionFailed:  CodePreconditionFailed,
	trpcgo.CodePayloadTooLarge:     CodePayloadTooLarge,
	trpcgo.CodeUnsupportedMedia:    CodeUnsupportedMedia,
	trpcgo.CodeUnprocessableContent: CodeUnprocessableContent,
	trpcgo.CodePreconditionRequired: CodeBadRequest,
	trpcgo.CodeTooManyRequests:     CodeTooManyRequests,
	trpcgo.CodeClientClosed:        CodeClientClosed,
	trpcgo.CodeNotImplemented:      CodeNotImplemented,
	trpcgo.CodeBadGateway:          CodeBadGateway,
	trpcgo.CodeServiceUnavailable:  CodeServiceUnavailable,
	trpcgo.CodeGatewayTimeout:      CodeGatewayTimeout,
}

// CodeFromTRPC converts a trpcgo ErrorCode to an oRPC code string.
func CodeFromTRPC(trpcCode trpcgo.ErrorCode) string {
	if code, ok := trpcCodeToORPC[trpcCode]; ok {
		return code
	}
	return CodeInternalServerError
}
