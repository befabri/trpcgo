package trpcgo

import (
	"context"
	"encoding/json"
	"runtime"
)

// resultEnvelope is the success response envelope: {"result":{"data":...}}
type resultEnvelope struct {
	Result resultData `json:"result"`
}

type resultData struct {
	Data any `json:"data"`
}

// ErrorEnvelope is the error response envelope following JSON-RPC 2.0 conventions.
// It is exposed so custom error formatters can inspect or extend the default shape.
type ErrorEnvelope struct {
	Error ErrorShape `json:"error"`
}

// ErrorShape is the error object within an ErrorEnvelope.
type ErrorShape struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Data    ErrorShapeData `json:"data"`
}

// ErrorShapeData contains machine-readable error metadata.
type ErrorShapeData struct {
	Code       string `json:"code"`
	HTTPStatus int    `json:"httpStatus"`
	Path       string `json:"path,omitempty"`
	Stack      string `json:"stack,omitempty"`
}

func newResultEnvelope(data any) resultEnvelope {
	return resultEnvelope{Result: resultData{Data: data}}
}

// defaultErrorEnvelope builds the standard tRPC error envelope.
func defaultErrorEnvelope(err *Error, path string, isDev bool) ErrorEnvelope {
	httpStatus := HTTPStatusFromCode(err.Code)
	data := ErrorShapeData{
		Code:       NameFromCode(err.Code),
		HTTPStatus: httpStatus,
		Path:       path,
	}
	if isDev {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		data.Stack = string(buf[:n])
	}
	return ErrorEnvelope{
		Error: ErrorShape{
			Code:    err.Code,
			Message: err.Message,
			Data:    data,
		},
	}
}

// formatError builds the error response, applying the custom error formatter if configured.
func formatError(opts *routerOptions, err *Error, path string, input json.RawMessage, ctx context.Context, typ ProcedureType) any {
	err = sanitizeErrorForClient(err)
	shape := defaultErrorEnvelope(err, path, opts.isDev)
	if opts.errorFormatter == nil {
		return shape
	}
	return opts.errorFormatter(ErrorFormatterInput{
		Error: err,
		Type:  typ,
		Path:  path,
		Input: input,
		Ctx:   ctx,
		Shape: shape,
	})
}
