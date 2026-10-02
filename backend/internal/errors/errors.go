package errors

import (
	"errors"
	"fmt"
)

type ErrorCode string

const (
	CodeInvalidInput       ErrorCode = "INVALID_INPUT"
	CodeNotFound           ErrorCode = "NOT_FOUND"
	CodeUnauthorized       ErrorCode = "UNAUTHORIZED"
	CodeForbidden          ErrorCode = "FORBIDDEN"
	CodeUpstreamError      ErrorCode = "UPSTREAM_ERROR"
	CodeCacheError         ErrorCode = "CACHE_ERROR"
	CodeDatabaseError      ErrorCode = "DATABASE_ERROR"
	CodeInternalError      ErrorCode = "INTERNAL_ERROR"
	CodeConflict           ErrorCode = "CONFLICT"
	CodeServiceUnavailable ErrorCode = "SERVICE_UNAVAILABLE"
)

type AppError struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Cause   error     `json:"-"`
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *AppError) Unwrap() error {
	return e.Cause
}

func New(code ErrorCode, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func Wrap(code ErrorCode, message string, cause error) *AppError {
	return &AppError{Code: code, Message: message, Cause: cause}
}

func IsCode(err error, code ErrorCode) bool {
	if appErr, ok := errors.AsType[*AppError](err); ok {
		return appErr.Code == code
	}
	return false
}

func GetCode(err error) ErrorCode {
	if err == nil {
		return ""
	}
	if appErr, ok := errors.AsType[*AppError](err); ok {
		return appErr.Code
	}
	return CodeInternalError
}

func GetMessage(err error) string {
	if err == nil {
		return ""
	}
	if appErr, ok := errors.AsType[*AppError](err); ok {
		return appErr.Message
	}
	return err.Error()
}
