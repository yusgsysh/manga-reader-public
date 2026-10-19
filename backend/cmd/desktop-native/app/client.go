package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to the Manga Reader HTTP API. The desktop app starts the same
// Gin server in-process eventually; until then it points at a running backend,
// configured with MANGA_READER_API (default http://127.0.0.1:8080).
type Client struct {
	baseURL  string
	user     string
	password string
	http     *http.Client
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

func (c *Client) BaseURL() string { return c.baseURL }

// do sends one request and decodes a JSON response into out when it is not
// nil. A non-2xx response becomes an error carrying the API's message.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body io.Reader, out any) error {
	target := c.baseURL + path
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
	return c.pages(ctx, fmt.Sprintf("/api/gallery/%d/%s/pages", id, url.PathEscape(token)), 10*time.Minute)
}

func (c *Client) CachedPages(ctx context.Context, id int64, token string) (int, []string, error) {
	return c.pages(ctx, fmt.Sprintf("/api/gallery-cache/%d/%s/pages", id, url.PathEscape(token)), 10*time.Minute)
}

func (c *Client) pages(ctx context.Context, path string, timeout time.Duration) (int, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return 0, nil, err
	}
	if c.user != "" {
		req.SetBasicAuth(c.user, c.password)
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

func (c *Client) UpdateProgress(ctx context.Context, id int64, token string, page int, progress float64, completed bool) error {
	body := strings.NewReader(fmt.Sprintf(
		`{"current_page":%d,"progress":%f,"completed":%t}`, page, progress, completed))
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return c.do(ctx, http.MethodPut, fmt.Sprintf("/api/progress/%d/%s", id, url.PathEscape(token)), nil, body, nil)
}

// --- Images ---
//
// The live proxies need no MinIO; the cached ones are used once the desktop
// cache is wired in. Both take the upstream URL as a query parameter.

func (c *Client) ThumbnailURL(source string) string {
	if source == "" {
		return ""
	}
	return c.baseURL + "/api/image/thumbnail?url=" + url.QueryEscape(source)
}

func (c *Client) PageImageURL(source string) string {
	if source == "" {
		return ""
	}
	return c.baseURL + "/api/image/page?url=" + url.QueryEscape(source)
}

func (c *Client) PageThumbnailURL(id int64, token string, index int) string {
	return c.baseURL + fmt.Sprintf("/api/image/page-thumbnail?id=%d&token=%s&index=%d",
		id, url.QueryEscape(token), index)
}
