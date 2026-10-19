package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"manga-reader/internal/config"
)

// Client talks to the Manga Reader HTTP API. In the desktop app it points at
// the in-process server; MANGA_READER_API can point it at an external one.
//
// The base URL is mutable so the first-run flow can start the embedded server
// and re-point the client at it without rebuilding the app.
type Client struct {
	mu       sync.RWMutex
	baseURL  string
	user     string
	password string
	http     *http.Client

	// authToken is the embedded server's per-run token; empty when talking
	// to an external backend (which may use Basic Auth instead).
	authToken string

	// imageCache routes image requests through the read-through cache proxy
	// (/api/image-cache/*). The embedded desktop server always provides it;
	// against a bare external backend the live proxies are used instead.
	imageCache bool
}

func NewClient(rawBaseURL string) *Client {
	base := strings.TrimRight(strings.TrimSpace(rawBaseURL), "/")
	user, password := "", ""
	if u, err := url.Parse(base); err == nil && u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
		u.User = nil
		base = strings.TrimRight(u.String(), "/")
	}
	return &Client{
		baseURL:  base,
		user:     user,
		password: password,
		http:     &http.Client{},
	}
}

func (c *Client) BaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseURL
}

// SetBaseURL re-points the client at another backend. It is safe to call
// while requests are in flight: they finish against the address they started
// with, and later calls use the new one.
func (c *Client) SetBaseURL(rawBaseURL string) {
	base := strings.TrimRight(strings.TrimSpace(rawBaseURL), "/")
	c.mu.Lock()
	c.baseURL = base
	c.mu.Unlock()
}

// UseImageCache enables or disables the read-through cache proxy for images.
func (c *Client) UseImageCache(enabled bool) {
	c.mu.Lock()
	c.imageCache = enabled
	c.mu.Unlock()
}

// SetAuthToken sets the embedded server's per-run token sent with every
// request. An empty token (external backends) clears it.
func (c *Client) SetAuthToken(token string) {
	c.mu.Lock()
	c.authToken = token
	c.mu.Unlock()
}

func (c *Client) authTokenValue() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authToken
}

// imagePath returns the cache or live image proxy path for kind.
func (c *Client) imagePath(kind string) string {
	c.mu.RLock()
	cached := c.imageCache
	c.mu.RUnlock()
	if cached {
		return "/api/image-cache/" + kind
	}
	return "/api/image/" + kind
}

// do sends one request and decodes a JSON response into out when it is not
// nil. A non-2xx response becomes an error carrying the API's message.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, out any) error {
	target := c.BaseURL() + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	if token := c.authTokenValue(); token != "" {
		req.Header.Set(config.AuthHeader, token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接后端失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("后端返回 %d：%s", resp.StatusCode, apiError(msg))
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("解析后端响应失败：%w", err)
	}
	return nil
}

func apiError(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return strings.TrimSpace(string(body))
}

// --- List endpoints ---

type apiGalleryItem struct {
	ID        int64    `json:"id"`
	Token     string   `json:"token"`
	Title     string   `json:"title"`
	TitleJPN  string   `json:"title_jpn"`
	Category  string   `json:"category"`
	Cover     string   `json:"cover"`
	Thumbnail string   `json:"thumbnail"`
	Pages     int      `json:"pages"`
	PageCount int      `json:"page_count"`
	Rating    float64  `json:"rating"`
	Posted    string   `json:"posted"`
	Tags      []string `json:"tags"`
	Uploader  string   `json:"uploader"`
}

type apiListResponse struct {
	Page       int              `json:"page"`
	PageSize   int              `json:"page_size"`
	Total      int              `json:"total"`
	TotalPages int              `json:"total_pages"`
	Results    []apiGalleryItem `json:"results"`
}

func (c *Client) List(ctx context.Context, path string, page int, query url.Values) (apiListResponse, error) {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("page", strconv.Itoa(page))
	var out apiListResponse
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, path, q, nil, &out)
	return out, err
}

// --- Gallery detail ---

type apiTag struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type apiGalleryDetail struct {
	ID          int64    `json:"id"`
	Token       string   `json:"token"`
	Title       string   `json:"title"`
	TitleJPN    string   `json:"title_jpn"`
	Category    string   `json:"category"`
	Thumbnail   string   `json:"thumbnail"`
	Cover       string   `json:"cover"`
	PageCount   int      `json:"page_count"`
	Rating      float64  `json:"rating"`
	RatingCount int      `json:"rating_count"`
	Uploader    string   `json:"uploader"`
	PostedAt    string   `json:"posted_at"`
	Tags        []apiTag `json:"tags"`
	FileSize    string   `json:"file_size"`
	Expunged    bool     `json:"expunged"`
}

func (c *Client) Gallery(ctx context.Context, id int64, token string) (apiGalleryDetail, error) {
	var out apiGalleryDetail
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/gallery/%d/%s", id, url.PathEscape(token)), nil, nil, &out)
	return out, err
}

func (c *Client) CachedGallery(ctx context.Context, id int64, token string) (apiGalleryDetail, error) {
	var out apiGalleryDetail
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/gallery-cache/%d/%s", id, url.PathEscape(token)), nil, nil, &out)
	return out, err
}

// --- Page list (NDJSON stream) ---

func (c *Client) Pages(ctx context.Context, id int64, token string) (int, []string, error) {
	return c.pages(ctx, fmt.Sprintf("/api/gallery/%d/%s/pages", id, url.PathEscape(token)), 10*time.Minute, nil)
}

func (c *Client) CachedPages(ctx context.Context, id int64, token string) (int, []string, error) {
	return c.pages(ctx, fmt.Sprintf("/api/gallery-cache/%d/%s/pages", id, url.PathEscape(token)), 10*time.Minute, nil)
}

// PageStreamFunc hears a page list as it arrives. url is the page's URL and
// index its position; total is the gallery's page count when the backend
// announced it (upstream counts it up front), else 0.
type PageStreamFunc func(index int, pageURL string, total int)

// PagesStreaming reads the page list like Pages, but hands each page to onPage
// as the backend scrapes it. A large gallery takes tens of seconds upstream,
// and the reader can show what has arrived instead of waiting for the last
// page. onPage runs on the caller's goroutine, so it must not touch the UI
// directly.
func (c *Client) PagesStreaming(ctx context.Context, id int64, token string, onPage PageStreamFunc) (int, []string, error) {
	return c.pages(ctx, fmt.Sprintf("/api/gallery/%d/%s/pages", id, url.PathEscape(token)), 10*time.Minute, onPage)
}

// CachedPagesStreaming is PagesStreaming against the offline copy.
func (c *Client) CachedPagesStreaming(ctx context.Context, id int64, token string, onPage PageStreamFunc) (int, []string, error) {
	return c.pages(ctx, fmt.Sprintf("/api/gallery-cache/%d/%s/pages", id, url.PathEscape(token)), 10*time.Minute, onPage)
}

func (c *Client) pages(ctx context.Context, path string, timeout time.Duration, onPage PageStreamFunc) (int, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
	}
	// The embedded server requires its per-run token on every request; a
	// hand-built request must carry it as Client.do does, or the page list
	// is answered 401 and the reader never loads.
	if token := c.authTokenValue(); token != "" {
		req.Header.Set(config.AuthHeader, token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("连接后端失败：%w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, nil, fmt.Errorf("后端返回 %d：%s", resp.StatusCode, apiError(msg))
	}

	type line struct {
		Type    string `json:"type"`
		Total   int    `json:"total"`
		PageURL string `json:"page_url"`
		Index   int    `json:"index"`
		Error   string `json:"error"`
	}

	var (
		total int
		urls  []string
		done  bool
		scan  = bufio.NewScanner(resp.Body)
	)
	scan.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scan.Scan() {
		var l line
		if err := json.Unmarshal(scan.Bytes(), &l); err != nil {
			return 0, nil, fmt.Errorf("解析页面流失败：%w", err)
		}
		switch l.Type {
		case "meta":
			total = l.Total
		case "page":
			if l.Index != len(urls) {
				return 0, nil, fmt.Errorf("页面流索引不连续：got %d, want %d", l.Index, len(urls))
			}
			urls = append(urls, l.PageURL)
			if onPage != nil {
				onPage(l.Index, l.PageURL, total)
			}
		case "done":
			if l.Total > 0 {
				total = l.Total
			}
			done = true
		case "error":
			return 0, nil, fmt.Errorf("抓取页面失败：%s", l.Error)
		}
	}
	if err := scan.Err(); err != nil {
		return 0, nil, fmt.Errorf("读取页面流失败：%w", err)
	}
	if !done {
		return 0, nil, fmt.Errorf("页面流提前结束")
	}
	if total == 0 {
		total = len(urls)
	}
	return total, urls, nil
}

// --- Bookshelf and progress ---

type apiReading struct {
	CurrentPage int     `json:"current_page"`
	Progress    float64 `json:"progress"`
	Completed   bool    `json:"completed"`
}

type apiShelfItem struct {
	ID        int64       `json:"id"`
	Token     string      `json:"token"`
	Title     string      `json:"title"`
	TitleJPN  string      `json:"title_jpn"`
	Category  string      `json:"category"`
	Thumbnail string      `json:"thumbnail"`
	Pages     int         `json:"pages"`
	Reading   *apiReading `json:"reading"`
}

type apiShelfResponse struct {
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	Total      int            `json:"total"`
	TotalPages int            `json:"total_pages"`
	Results    []apiShelfItem `json:"results"`
}

func (c *Client) Bookshelf(ctx context.Context, page int) (apiShelfResponse, error) {
	var out apiShelfResponse
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, "/api/bookshelf", url.Values{"page": {strconv.Itoa(page)}}, nil, &out)
	return out, err
}

func (c *Client) RecentlyRead(ctx context.Context, page int) (apiShelfResponse, error) {
	var out apiShelfResponse
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, "/api/recently-read", url.Values{"page": {strconv.Itoa(page)}}, nil, &out)
	return out, err
}

func (c *Client) BookshelfStatus(ctx context.Context, id int64, token string) (bool, error) {
	var out struct {
		InBookshelf bool `json:"in_bookshelf"`
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/bookshelf/%d/%s/status", id, url.PathEscape(token)), nil, nil, &out)
	return out.InBookshelf, err
}

func (c *Client) AddBookshelf(ctx context.Context, id int64, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/api/bookshelf/%d/%s", id, url.PathEscape(token)), nil, nil, nil)
}

func (c *Client) RemoveBookshelf(ctx context.Context, id int64, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/api/bookshelf/%d/%s", id, url.PathEscape(token)), nil, nil, nil)
}

func (c *Client) Progress(ctx context.Context, id int64, token string) (apiReading, error) {
	var out apiReading
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/api/progress/%d/%s", id, url.PathEscape(token)), nil, nil, &out)
	return out, err
}

// --- Tag rules ---

type apiTagRule struct {
	Tag    string `json:"tag"`
	Action string `json:"action"`
	Weight int    `json:"weight"`
}

type apiTagSyncConflict struct {
	Tag    string      `json:"tag"`
	Local  *apiTagRule `json:"local"`
	Remote *apiTagRule `json:"remote"`
}

type apiTagSyncFailure struct {
	Tag   string `json:"tag"`
	Error string `json:"error"`
}

type apiTagSyncResult struct {
	Added          int                  `json:"added"`
	Updated        int                  `json:"updated"`
	Deleted        int                  `json:"deleted"`
	Pulled         int                  `json:"pulled"`
	Unchanged      int                  `json:"unchanged"`
	PendingDeletes []string             `json:"pending_deletes"`
	Conflicts      []apiTagSyncConflict `json:"conflicts"`
	Failed         []apiTagSyncFailure  `json:"failed"`
	Tags           []apiTagRule         `json:"tags"`
}

// TagSettings lists every tag rule.
func (c *Client) TagSettings(ctx context.Context) ([]TagRule, error) {
	var out struct {
		Tags []apiTagRule `json:"tags"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/tag-settings", nil, nil, &out); err != nil {
		return nil, err
	}
	return tagRulesFromAPI(out.Tags), nil
}

// SaveTagSettings replaces every rule with rules and returns what the backend
// stored (normalised, defaulted and sorted).
func (c *Client) SaveTagSettings(ctx context.Context, rules []TagRule) ([]TagRule, error) {
	payload := struct {
		Tags []apiTagRule `json:"tags"`
	}{Tags: make([]apiTagRule, 0, len(rules))}
	for _, r := range rules {
		payload.Tags = append(payload.Tags, apiTagRule{Tag: r.Tag, Action: string(r.Action), Weight: r.Weight})
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tags []apiTagRule `json:"tags"`
	}
	if err := c.do(ctx, http.MethodPut, "/api/tag-settings", nil, bytes.NewReader(body), &out); err != nil {
		return nil, err
	}
	return tagRulesFromAPI(out.Tags), nil
}

// SyncTagSettings merges the local rules with the source's My Tags, optionally
// confirming source deletions and resolving every conflict one way.
func (c *Client) SyncTagSettings(ctx context.Context, confirmDeletes bool, resolution string) (apiTagSyncResult, error) {
	body, err := json.Marshal(map[string]any{
		"confirm_deletes":     confirmDeletes,
		"conflict_resolution": resolution,
	})
	if err != nil {
		return apiTagSyncResult{}, err
	}
	var out apiTagSyncResult
	if err := c.do(ctx, http.MethodPost, "/api/tag-settings/sync", nil, bytes.NewReader(body), &out); err != nil {
		return apiTagSyncResult{}, err
	}
	return out, nil
}

func tagRulesFromAPI(in []apiTagRule) []TagRule {
	out := make([]TagRule, 0, len(in))
	for _, r := range in {
		out = append(out, TagRule{
			Tag:    r.Tag,
			Action: TagAction(r.Action),
			Weight: r.Weight,
		})
	}
	return out
}

// --- Torrents ---

type apiTorrent struct {
	GTID      string `json:"gtid"`
	Name      string `json:"name"`
	Size      string `json:"size"`
	Posted    string `json:"posted"`
	Seeds     int    `json:"seeds"`
	Peers     int    `json:"peers"`
	Downloads int    `json:"downloads"`
	Uploader  string `json:"uploader"`
}

// Torrents lists a gallery's torrents (scraped live, never cached).
func (c *Client) Torrents(ctx context.Context, id int64, token string) ([]Torrent, error) {
	var out struct {
		Torrents []apiTorrent `json:"torrents"`
	}
	path := fmt.Sprintf("/api/gallery/%d/%s/torrents", id, url.PathEscape(token))
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return nil, err
	}
	items := make([]Torrent, 0, len(out.Torrents))
	for _, t := range out.Torrents {
		items = append(items, Torrent{
			GTID: t.GTID, Name: t.Name, Size: t.Size, Posted: t.Posted,
			Seeds: t.Seeds, Peers: t.Peers, Downloads: t.Downloads, Uploader: t.Uploader,
		})
	}
	return items, nil
}

// DownloadTorrent saves a gallery's .torrent file to path.
func (c *Client) DownloadTorrent(ctx context.Context, id int64, token, gtid, path string) (int64, error) {
	apiPath := fmt.Sprintf("/api/gallery/%d/%s/torrents/%s/download?variant=redistributable",
		id, url.PathEscape(token), url.PathEscape(gtid))
	return c.downloadTo(path, apiPath, 2*time.Minute)
}

// --- Prefill (offline downloads) ---
//
// A prefill job fetches the gallery's pages into the backend's image cache
// one at a time, so the gallery can be read with no network. Jobs are kept by
// the backend; the UI only starts and polls them.

type PrefillProgress struct {
	Done    int `json:"done"`
	Cached  int `json:"cached"`
	Fetched int `json:"fetched"`
}

type PrefillError struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
	Error string `json:"error"`
}

type PrefillJob struct {
	ID          string           `json:"id"`
	GalleryID   int64            `json:"gallery_id"`
	Token       string           `json:"token"`
	Title       string           `json:"title"`
	Status      string           `json:"status"`
	Total       int              `json:"total"`
	Progress    *PrefillProgress `json:"progress"`
	FailedCount int              `json:"failed_count"`
	Errors      []PrefillError   `json:"errors"`
	CreatedAt   string           `json:"created_at"`
	UpdatedAt   string           `json:"updated_at"`
	FinishedAt  *string          `json:"finished_at"`
}

// StartPrefill queues a job for urls. A gallery already queued or running
// returns that job instead of a new one.
func (c *Client) StartPrefill(ctx context.Context, id int64, token, title string, urls []string) (PrefillJob, error) {
	payload, err := json.Marshal(map[string]any{
		"gallery_id": id,
		"token":      token,
		"title":      title,
		"urls":       urls,
	})
	if err != nil {
		return PrefillJob{}, err
	}
	var out PrefillJob
	if err := c.do(ctx, http.MethodPost, "/api/prefill", nil, bytes.NewReader(payload), &out); err != nil {
		return PrefillJob{}, err
	}
	return out, nil
}

// PrefillJobs lists the most recent jobs.
func (c *Client) PrefillJobs(ctx context.Context) ([]PrefillJob, error) {
	var out struct {
		Jobs []PrefillJob `json:"jobs"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/prefill", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Jobs, nil
}

// PrefillJob fetches one job, for polling its progress.
func (c *Client) PrefillJob(ctx context.Context, jobID string) (PrefillJob, error) {
	var out PrefillJob
	path := "/api/prefill/" + url.PathEscape(jobID)
	if err := c.do(ctx, http.MethodGet, path, nil, nil, &out); err != nil {
		return PrefillJob{}, err
	}
	return out, nil
}

// CancelPrefill stops a queued or running job.
func (c *Client) CancelPrefill(ctx context.Context, jobID string) (PrefillJob, error) {
	var out PrefillJob
	path := "/api/prefill/" + url.PathEscape(jobID) + "/cancel"
	if err := c.do(ctx, http.MethodPost, path, nil, nil, &out); err != nil {
		return PrefillJob{}, err
	}
	return out, nil
}

// DeletePrefill removes a finished job's record.
func (c *Client) DeletePrefill(ctx context.Context, jobID string) error {
	return c.do(ctx, http.MethodDelete, "/api/prefill/"+url.PathEscape(jobID), nil, nil, nil)
}

func (c *Client) UpdateProgress(ctx context.Context, id int64, token string, page int, progress float64, completed bool) error {
	body := strings.NewReader(fmt.Sprintf(
		`{"current_page":%d,"progress":%f,"completed":%t}`, page, progress, completed))
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/progress/%d/%s", id, url.PathEscape(token)), nil, body, nil)
}

// --- Images ---
//
// Both the live and the read-through cache proxies take the upstream URL as a
// query parameter, so switching between them is just a path prefix. The
// embedded desktop server always has a cache; an external backend may not
// (its cached endpoints return 503 without MinIO).

func (c *Client) ThumbnailURL(source string) string {
	if source == "" {
		return ""
	}
	return c.BaseURL() + c.imagePath("thumbnail") + "?url=" + url.QueryEscape(source)
}

func (c *Client) PageImageURL(source string) string {
	if source == "" {
		return ""
	}
	return c.BaseURL() + c.imagePath("page") + "?url=" + url.QueryEscape(source)
}

func (c *Client) PageThumbnailURL(id int64, token string, index int) string {
	return c.BaseURL() + fmt.Sprintf("%s?id=%d&token=%s&index=%d",
		c.imagePath("page-thumbnail"), id, url.QueryEscape(token), index)
}
