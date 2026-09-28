package exhentai

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	json "encoding/json/v2"

	"manga-reader/internal/model"
)

const apiURL = "https://api.e-hentai.org/api.php"

// PostGalleryMetadata calls the official API to get gallery metadata.
func PostGalleryMetadata(ctx context.Context, client *http.Client, gid int64, token string) (*model.GalleryMetadata, error) {
	type request struct {
		Method    string  `json:"method"`
		GIdList   [][]any `json:"gidlist"`
		Namespace int     `json:"namespace"`
	}
	type response struct {
		GMetadata []model.GalleryMetadata `json:"gmetadata"`
	}

	reqBody := request{
		Method:    "gdata",
		GIdList:   [][]any{{gid, token}},
		Namespace: 1,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: HTTP %d", ErrNonOKStatus, resp.StatusCode)
	}

	var result response
	if err := json.UnmarshalRead(resp.Body, &result); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrParsingFailed, err)
	}

	if len(result.GMetadata) == 0 {
		return nil, ErrNoMetadata
	}

	meta := &result.GMetadata[0]
	if meta.Error != "" {
		return nil, fmt.Errorf("%w: %s", ErrAPIError, meta.Error)
	}

	return meta, nil
}
