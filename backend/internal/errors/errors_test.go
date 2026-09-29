package errors

import (
	"errors"
	"fmt"
	"testing"
)

func TestGetMessage_Nil(t *testing.T) {
	got := GetMessage(nil)
	if got != "" {
		t.Errorf("GetMessage(nil) = %q, want empty string", got)
	}
}

func TestGetCode_Nil(t *testing.T) {
	got := GetCode(nil)
	if got != "" {
		t.Errorf("GetCode(nil) = %q, want empty string", got)
	}
}

func TestGetMessage_AppError(t *testing.T) {
	appErr := New(CodeNotFound, "not found")
	got := GetMessage(appErr)
	if got != "not found" {
		t.Errorf("GetMessage(AppError) = %q, want %q", got, "not found")
	}
}

func TestGetMessage_WrappedAppError(t *testing.T) {
	appErr := New(CodeNotFound, "not found")
	wrapped := fmt.Errorf("context: %w", appErr)
	got := GetMessage(wrapped)
	if got != "not found" {
		t.Errorf("GetMessage(wrapped) = %q, want %q", got, "not found")
	}
}

func TestGetMessage_PlainError(t *testing.T) {
	plain := errors.New("plain error")
	got := GetMessage(plain)
	if got != "plain error" {
		t.Errorf("GetMessage(plain) = %q, want %q", got, "plain error")
	}
}

func TestGetCode_AppError(t *testing.T) {
	appErr := New(CodeForbidden, "forbidden")
	got := GetCode(appErr)
	if got != CodeForbidden {
		t.Errorf("GetCode(AppError) = %q, want %q", got, CodeForbidden)
	}
}

func TestAppError_Error(t *testing.T) {
	appErr := New(CodeNotFound, "not found")
	got := appErr.Error()
	want := "NOT_FOUND: not found"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestAppError_ErrorWithCause(t *testing.T) {
	cause := errors.New("root cause")
	appErr := Wrap(CodeUpstreamError, "upstream failed", cause)
	got := appErr.Error()
	want := "UPSTREAM_ERROR: upstream failed: root cause"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestAppError_Unwrap(t *testing.T) {
	cause := errors.New("root cause")
	appErr := Wrap(CodeUpstreamError, "upstream failed", cause)
	unwrapped := appErr.Unwrap()
	if unwrapped != cause {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, cause)
	}
}

func TestErrorsIs_ThroughCause(t *testing.T) {
	cause := errors.New("root cause")
	appErr := Wrap(CodeUpstreamError, "upstream failed", cause)
	if !errors.Is(appErr, cause) {
		t.Error("errors.Is should find cause through AppError chain")
	}
}

func TestErrorsAs_ThroughWrap(t *testing.T) {
	appErr := New(CodeNotFound, "not found")
	wrapped := fmt.Errorf("context: %w", appErr)
	var target *AppError
	if !errors.As(wrapped, &target) {
		t.Fatal("errors.As should find AppError through wrap chain")
	}
	if target.Code != CodeNotFound {
		t.Errorf("errors.As found code = %q, want %q", target.Code, CodeNotFound)
	}
}

func TestIsCode(t *testing.T) {
	appErr := New(CodeForbidden, "forbidden")
	if !IsCode(appErr, CodeForbidden) {
		t.Error("IsCode should return true for matching code")
	}
	if IsCode(appErr, CodeNotFound) {
		t.Error("IsCode should return false for non-matching code")
	}
}
