package exhentai

import (
	"errors"
	"fmt"
	"testing"

	apperrors "manga-reader/internal/errors"
)

func TestToAppError_Nil(t *testing.T) {
	got := ToAppError(nil)
	if got != nil {
		t.Errorf("ToAppError(nil) = %v, want nil", got)
	}
}

func TestToAppError_ExistingAppError(t *testing.T) {
	existing := apperrors.New(apperrors.CodeNotFound, "already app error")
	got := ToAppError(existing)
	if got != existing {
		t.Errorf("ToAppError(existing) = %v, want same pointer %v", got, existing)
	}
}

func TestToAppError_ErrImageTooLarge(t *testing.T) {
	got := ToAppError(ErrImageTooLarge)
	if got == nil {
		t.Fatal("ToAppError(ErrImageTooLarge) = nil")
	}
	if got.Code != apperrors.CodeUpstreamError {
		t.Errorf("ErrImageTooLarge code = %q, want %q", got.Code, apperrors.CodeUpstreamError)
	}
}

func TestToAppError_ErrIPBanned(t *testing.T) {
	got := ToAppError(ErrIPBanned)
	if got == nil {
		t.Fatal("ToAppError(ErrIPBanned) = nil")
	}
	if got.Code != apperrors.CodeForbidden {
		t.Errorf("ErrIPBanned code = %q, want %q", got.Code, apperrors.CodeForbidden)
	}
}

func TestToAppError_ErrSadPanda(t *testing.T) {
	got := ToAppError(ErrSadPanda)
	if got == nil {
		t.Fatal("ToAppError(ErrSadPanda) = nil")
	}
	if got.Code != apperrors.CodeServiceUnavailable {
		t.Errorf("ErrSadPanda code = %q, want %q", got.Code, apperrors.CodeServiceUnavailable)
	}
}

func TestToAppError_ErrNoMetadata(t *testing.T) {
	got := ToAppError(ErrNoMetadata)
	if got == nil {
		t.Fatal("ToAppError(ErrNoMetadata) = nil")
	}
	if got.Code != apperrors.CodeNotFound {
		t.Errorf("ErrNoMetadata code = %q, want %q", got.Code, apperrors.CodeNotFound)
	}
}

func TestToAppError_HTTP404(t *testing.T) {
	err := &httpStatusError{code: 404}
	got := ToAppError(err)
	if got == nil {
		t.Fatal("ToAppError(404) = nil")
	}
	if got.Code != apperrors.CodeNotFound {
		t.Errorf("HTTP 404 code = %q, want %q", got.Code, apperrors.CodeNotFound)
	}
}

func TestToAppError_HTTP403(t *testing.T) {
	err := &httpStatusError{code: 403}
	got := ToAppError(err)
	if got == nil {
		t.Fatal("ToAppError(403) = nil")
	}
	if got.Code != apperrors.CodeForbidden {
		t.Errorf("HTTP 403 code = %q, want %q", got.Code, apperrors.CodeForbidden)
	}
}

func TestToAppError_HTTP429(t *testing.T) {
	err := &httpStatusError{code: 429}
	got := ToAppError(err)
	if got == nil {
		t.Fatal("ToAppError(429) = nil")
	}
	if got.Code != apperrors.CodeServiceUnavailable {
		t.Errorf("HTTP 429 code = %q, want %q", got.Code, apperrors.CodeServiceUnavailable)
	}
}

func TestToAppError_HTTP500(t *testing.T) {
	err := &httpStatusError{code: 500}
	got := ToAppError(err)
	if got == nil {
		t.Fatal("ToAppError(500) = nil")
	}
	if got.Code != apperrors.CodeUpstreamError {
		t.Errorf("HTTP 500 code = %q, want %q", got.Code, apperrors.CodeUpstreamError)
	}
}

func TestToAppError_HTTP502(t *testing.T) {
	err := &httpStatusError{code: 502}
	got := ToAppError(err)
	if got == nil {
		t.Fatal("ToAppError(502) = nil")
	}
	if got.Code != apperrors.CodeUpstreamError {
		t.Errorf("HTTP 502 code = %q, want %q", got.Code, apperrors.CodeUpstreamError)
	}
}

func TestToAppError_HTTP503(t *testing.T) {
	err := &httpStatusError{code: 503}
	got := ToAppError(err)
	if got == nil {
		t.Fatal("ToAppError(503) = nil")
	}
	if got.Code != apperrors.CodeUpstreamError {
		t.Errorf("HTTP 503 code = %q, want %q", got.Code, apperrors.CodeUpstreamError)
	}
}

func TestToAppError_UnknownError(t *testing.T) {
	unknown := errors.New("something weird")
	got := ToAppError(unknown)
	if got == nil {
		t.Fatal("ToAppError(unknown) = nil")
	}
	if got.Code != apperrors.CodeInternalError {
		t.Errorf("unknown error code = %q, want %q", got.Code, apperrors.CodeInternalError)
	}
}

func TestToAppError_WrappedErrImageTooLarge(t *testing.T) {
	wrapped := fmt.Errorf("download failed: %w", ErrImageTooLarge)
	got := ToAppError(wrapped)
	if got == nil {
		t.Fatal("ToAppError(wrapped ErrImageTooLarge) = nil")
	}
	if got.Code != apperrors.CodeUpstreamError {
		t.Errorf("wrapped ErrImageTooLarge code = %q, want %q", got.Code, apperrors.CodeUpstreamError)
	}
}

func TestErrorsIs_ThroughToAppError(t *testing.T) {
	wrapped := fmt.Errorf("context: %w", ErrIPBanned)
	appErr := ToAppError(wrapped)
	if !errors.Is(appErr, ErrIPBanned) {
		t.Error("errors.Is should find ErrIPBanned through AppError chain")
	}
}
