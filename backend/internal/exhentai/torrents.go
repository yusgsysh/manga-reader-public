package exhentai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"manga-reader/internal/model"
)

// maxTorrentBytes caps a torrent download; real .torrent files are tiny (the
// source limits uploads to 10 MB).
const maxTorrentBytes = 10 << 20

// TorrentsURL is the ExHentai gallery-torrents page for a gallery.
func TorrentsURL(id, token string) string {
	return ExhentaiURL + "/gallerytorrents.php?gid=" + url.QueryEscape(id) + "&t=" + url.QueryEscape(token)
}

// TorrentInfoResult is the expanded "Information" view plus the raw personal
// and redistributable download links (not exposed to API clients; the handler
// proxies them).
type TorrentInfoResult struct {
	Info               model.GalleryTorrentInfo
	PersonalizedURL    string
	RedistributableURL string
}

func parseLooseInt(raw string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(raw))
	return n
}

// ScrapeGalleryTorrents fetches the gallery's torrents page and parses each
// torrent block. It returns an empty slice when the gallery has no torrents.
func ScrapeGalleryTorrents(ctx context.Context, client *http.Client, id, token string) ([]model.GalleryTorrent, error) {
	doc, err := httpGetDoc(ctx, client, TorrentsURL(id, token))
	if err != nil {
		return nil, err
	}

	torrents := make([]model.GalleryTorrent, 0)
	doc.Find("#torrentinfo form > div").Each(func(_ int, block *goquery.Selection) {
		gtidSel := block.Find("input[name=gtid]").First()
		if gtidSel.Length() == 0 {
			return
		}
		gtid, _ := gtidSel.Attr("value")
		torrent := model.GalleryTorrent{GTID: strings.TrimSpace(gtid)}

		block.Find("td").Each(func(_ int, td *goquery.Selection) {
			label := strings.TrimSpace(td.Find("span").First().Text())
			value := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(td.Text()), label))
			switch label {
			case "Posted:":
				torrent.Posted = value
			case "Size:":
				torrent.Size = value
			case "Seeds:":
				torrent.Seeds = parseLooseInt(value)
			case "Peers:":
				torrent.Peers = parseLooseInt(value)
			case "Downloads:":
				torrent.Downloads = parseLooseInt(value)
			case "Uploader:":
				torrent.Uploader = value
			}
		})

		if link := block.Find("a[href*='/torrent/']").First(); link.Length() > 0 {
			torrent.Name = strings.TrimSpace(link.Text())
			if href, ok := link.Attr("href"); ok {
				torrent.DownloadURL = strings.TrimSpace(href)
			}
		}

		if torrent.GTID != "" {
			torrents = append(torrents, torrent)
		}
	})

	return torrents, nil
}

// ScrapeGalleryTorrentInfo POSTs the "Information" action for one torrent and
// parses the stats/comments plus the personal and redistributable links.
func ScrapeGalleryTorrentInfo(ctx context.Context, client *http.Client, id, token, gtid string) (TorrentInfoResult, error) {
	form := url.Values{}
	form.Set("gtid", gtid)
	form.Set("torrent_info", "Information")

	doc, err := httpPostFormDoc(ctx, client, TorrentsURL(id, token), form)
	if err != nil {
		return TorrentInfoResult{}, err
	}

	var result TorrentInfoResult
	doc.Find("#ett tr").Each(func(_ int, tr *goquery.Selection) {
		cells := tr.Find("td")
		for i := 0; i+1 < cells.Length(); i += 2 {
			label := strings.TrimSpace(cells.Eq(i).Text())
			value := strings.TrimSpace(cells.Eq(i + 1).Text())
			switch label {
			case "Posted":
				result.Info.Posted = value
			case "Seeds":
				result.Info.Seeds = parseLooseInt(value)
			case "Uploader":
				result.Info.Uploader = value
			case "DLers":
				result.Info.Dlers = parseLooseInt(value)
			case "Size":
				result.Info.Size = value
			case "Completes":
				result.Info.Completes = parseLooseInt(value)
			}
		}
	})

	result.Info.Comments = strings.TrimSpace(doc.Find("#etd").Text())

	doc.Find("a").Each(func(_ int, a *goquery.Selection) {
		href, ok := a.Attr("href")
		if !ok || !strings.Contains(href, "/torrent/") {
			return
		}
		switch strings.TrimSpace(a.Text()) {
		case "Personalized Torrent":
			result.PersonalizedURL = href
			result.Info.Personalized = true
		case "Redistributable Torrent":
			result.RedistributableURL = href
		}
	})

	return result, nil
}

// FetchTorrent downloads a .torrent file from the upstream, live (no cache).
func FetchTorrent(ctx context.Context, client *http.Client, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || !isTorrentHost(u.Hostname()) {
		return nil, fmt.Errorf("refusing to fetch torrent from %q", u.Host)
	}

	ctx, cancel := context.WithTimeout(ctx, upstreamDocTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("torrent fetch failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxTorrentBytes {
		return nil, fmt.Errorf("torrent exceeds %d bytes", maxTorrentBytes)
	}
	return data, nil
}

func isTorrentHost(host string) bool {
	host = strings.ToLower(host)
	return host == "exhentai.org" || host == "e-hentai.org"
}

// TorrentFilename derives a safe .torrent filename from the torrent name.
func TorrentFilename(name, gtid string) string {
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".torrent")
	name = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', '"', '\r', '\n', '\t':
			return '_'
		}
		return r
	}, name)
	if name == "" {
		name = "torrent-" + gtid
	}
	return name + ".torrent"
}
