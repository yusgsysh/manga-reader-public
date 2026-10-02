package exhentai

import (
	"errors"
	"fmt"

	apperrors "manga-reader/internal/errors"
)

var (
	ErrInvalidGalleryID    = errors.New("invalid gallery id")
	ErrInvalidGalleryToken = errors.New("invalid gallery token")
	ErrRequestFailed       = errors.New("request failed")
	ErrNonOKStatus         = errors.New("non-ok status")
	ErrParsingFailed       = errors.New("parsing failed")
	ErrAPIError            = errors.New("api error")
	ErrSadPanda            = errors.New("sad panda")
	ErrIPBanned            = errors.New("ip banned")
	ErrNoMetadata          = errors.New("no metadata returned")
	ErrImageTooLarge       = errors.New("image too large")
)

// httpStatusError wraps a non-OK HTTP status for error classification.
type httpStatusError struct {
	code int
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %v", e.code, ErrNonOKStatus)
}

func (e *httpStatusError) Is(target error) bool {
	if target == ErrNonOKStatus {
		return true
	}
	_, ok := target.(*httpStatusError)
	return ok
}

// HTTPStatusError returns true if err is a non-OK HTTP status error.
func HTTPStatusError(err error) bool {
	var hse *httpStatusError
	return errors.As(err, &hse)
}

// HTTPStatusCode returns the HTTP status code from an error, or 0 if not a status error.
func HTTPStatusCode(err error) int {
	if hse, ok := errors.AsType[*httpStatusError](err); ok {
		return hse.code
	}
	return 0
}

// IsPermanentUpstreamError classifies errors that should not be retried.
func IsPermanentUpstreamError(err error) bool {
	if errors.Is(err, ErrIPBanned) || errors.Is(err, ErrSadPanda) || errors.Is(err, ErrImageTooLarge) {
		return true
	}
	if code := HTTPStatusCode(err); code != 0 {
		// 4xx (except 408, 429) are permanent; 5xx are transient
		if code >= 400 && code < 500 && code != 408 && code != 429 {
			return true
		}
	}
	return false
}

// ToAppError converts an exhentai error to an AppError with appropriate code.
func ToAppError(err error) *apperrors.AppError {
	if err == nil {
		return nil
	}

	if appErr, ok := errors.AsType[*apperrors.AppError](err); ok {
		return appErr
	}

	switch {
	case errors.Is(err, ErrIPBanned):
		return apperrors.Wrap(apperrors.CodeForbidden, "IP banned", err)
	case errors.Is(err, ErrSadPanda):
		return apperrors.Wrap(apperrors.CodeServiceUnavailable, "sad panda", err)
	case errors.Is(err, ErrNoMetadata):
		return apperrors.Wrap(apperrors.CodeNotFound, "no metadata", err)
	case errors.Is(err, ErrImageTooLarge):
		return apperrors.Wrap(apperrors.CodeUpstreamError, "image too large", err)
	case HTTPStatusError(err):
		code := HTTPStatusCode(err)
		switch {
		case code == 404:
			return apperrors.Wrap(apperrors.CodeNotFound, "resource not found", err)
		case code == 401:
			return apperrors.Wrap(apperrors.CodeUnauthorized, "unauthorized", err)
		case code == 403:
			return apperrors.Wrap(apperrors.CodeForbidden, "forbidden", err)
		case code == 429:
			return apperrors.Wrap(apperrors.CodeServiceUnavailable, "rate limited", err)
		case code >= 500:
			return apperrors.Wrap(apperrors.CodeUpstreamError, "upstream server error", err)
		default:
			return apperrors.Wrap(apperrors.CodeUpstreamError, "upstream client error", err)
		}
	default:
		return apperrors.Wrap(apperrors.CodeInternalError, "internal error", err)
	}
}
