package exhentai

import (
	"errors"
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
)
