package trpcgo

import "runtime"

// resultEnvelope is the success response envelope: {"result":{"data":...}}
type resultEnvelope struct {
	Result resultData `json:"result"`
}

type resultData struct {
	Data any `json:"data"`
}

// errorEnvelope is the error response envelope following JSON-RPC 2.0 conventions.
type errorEnvelope struct {
	Error errorShape `json:"error"`
}

type errorShape struct {
	Code    ErrorCode      `json:"code"`
	Message string         `json:"message"`
	Data    errorShapeData `json:"data"`
}

type errorShapeData struct {
	Code       string `json:"code"`
	HTTPStatus int    `json:"httpStatus"`
	Path       string `json:"path,omitempty"`
	Stack      string `json:"stack,omitempty"`
}

func newResultEnvelope(data any) resultEnvelope {
	return resultEnvelope{Result: resultData{Data: data}}
}

func newErrorEnvelope(err *Error, path string, isDev bool) errorEnvelope {
	httpStatus := HTTPStatusFromCode(err.Code)
	data := errorShapeData{
		Code:       NameFromCode(err.Code),
		HTTPStatus: httpStatus,
		Path:       path,
	}
	if isDev {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		data.Stack = string(buf[:n])
	}
	return errorEnvelope{
		Error: errorShape{
			Code:    err.Code,
			Message: err.Message,
			Data:    data,
		},
	}
}
