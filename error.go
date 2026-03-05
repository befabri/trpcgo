package trpcgo

import (
	"errors"
	"fmt"
)

// Error represents a tRPC error with a JSON-RPC 2.0 error code.
type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("trpc error %s: %s: %v", NameFromCode(e.Code), e.Message, e.Cause)
	}
	return fmt.Sprintf("trpc error %s: %s", NameFromCode(e.Code), e.Message)
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// NewError creates a new tRPC error.
func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

// NewErrorf creates a new tRPC error with a formatted message.
func NewErrorf(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// WrapError creates a new tRPC error wrapping a cause.
func WrapError(code ErrorCode, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

func sanitizeErrorForClient(err *Error) *Error {
	if err == nil {
		return nil
	}
	if err.Code == CodeInternalServerError && err.Cause != nil {
		return NewError(CodeInternalServerError, "internal server error")
	}
	return err
}

func sanitizeReturnedError(err error) *Error {
	if err == nil {
		return nil
	}
	if trpcErr, ok := errors.AsType[*Error](err); ok {
		return sanitizeErrorForClient(trpcErr)
	}
	return NewError(CodeInternalServerError, "internal server error")
}
