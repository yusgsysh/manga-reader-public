package exhentai

import (
	"errors"
	"fmt"
)

var (
	ErrRequestFailed = errors.New("request failed")
	ErrNonOKStatus   = errors.New("non-ok status")
	ErrParsingFailed = errors.New("parsing failed")
	ErrAPIError      = errors.New("api error")
	ErrSadPanda      = errors.New("sad panda")
	ErrIPBanned      = errors.New("ip banned")
	ErrNoMetadata    = errors.New("no metadata returned")
	ErrImageTooLarge = errors.New("image too large")
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
