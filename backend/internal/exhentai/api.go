package exhentai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"

	json "encoding/json/v2"

	"manga-reader/internal/metrics"
	"manga-reader/internal/model"
)

const apiURL = "https://api.e-hentai.org/api.php"

// PostGalleryMetadata calls the official API to get gallery metadata. Identical
// metadata lookups share one upstream request for a short window (see
// fetchOnce), so the online and cache metadata paths do not both hit gdata.
func PostGalleryMetadata(ctx context.Context, client *http.Client, gid int64, token string) (*model.GalleryMetadata, error) {
	key := fetchCacheKey(client, "GDATA", fmt.Sprintf("%d:%s", gid, token))
	value, err := fetchOnce(ctx, client, key, func(fetchCtx context.Context) (any, error) {
		return postGalleryMetadata(fetchCtx, client, gid, token)
	})
	if err != nil {
		return nil, err
	}
	// Return a copy so callers can never mutate the shared cached value.
	meta := value.(model.GalleryMetadata)
	return &meta, nil
}

func postGalleryMetadata(ctx context.Context, client *http.Client, gid int64, token string) (model.GalleryMetadata, error) {
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
		return model.GalleryMetadata{}, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", apiURL, bytes.NewReader(b))
	if err != nil {
		return model.GalleryMetadata{}, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return model.GalleryMetadata{}, fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		metrics.ObserveClassification(metrics.Endpoint(apiURL), metrics.OutcomeHTTPError, resp.StatusCode)
		return model.GalleryMetadata{}, &httpStatusError{code: resp.StatusCode}
	}

	var result response
	// Bound the JSON body like the HTML paths: the upstream response is
	// gunzipped transparently, so cap decompressed bytes before decoding.
	if err := json.UnmarshalRead(io.LimitReader(resp.Body, maxDocBytes), &result); err != nil {
		return model.GalleryMetadata{}, fmt.Errorf("%w: %v", ErrParsingFailed, err)
	}

	if len(result.GMetadata) == 0 {
		return model.GalleryMetadata{}, ErrNoMetadata
	}

	meta := result.GMetadata[0]
	if meta.Error != "" {
		return model.GalleryMetadata{}, fmt.Errorf("%w: %s", ErrAPIError, meta.Error)
	}

	return meta, nil
}
