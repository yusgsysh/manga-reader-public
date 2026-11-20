package handler

import (
	"fmt"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/httpx"
	"manga-reader/internal/model"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// gtidRe bounds the torrent id path parameter: it is echoed into a
// Content-Disposition filename and forwarded upstream as a form value.
var gtidRe = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// handleGalleryTorrents lists the gallery's torrents. It always scrapes the
// upstream torrents page and never touches the gallery cache.
func (s *Server) handleGalleryTorrents(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	client, uid, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	base := s.upstreamBase(c.Request.Context(), uid)
	torrents, err := exhentai.ScrapeGalleryTorrents(c.Request.Context(), client, base, strconv.FormatInt(id, 10), token)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery torrents failed: %v", err)})
		return
	}
	c.Header("Cache-Control", "no-store")
	httpx.JSON(c, http.StatusOK, gin.H{"torrents": torrents})
}

// handleGalleryTorrentInfo returns one torrent's expanded "Information" view
// (tracker stats plus the uploader comment). Live scrape, no cache.
func (s *Server) handleGalleryTorrentInfo(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	gtid := strings.TrimSpace(c.Param("gtid"))
	if !gtidRe.MatchString(gtid) {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid torrent id"})
		return
	}
	client, uid, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	result, err := exhentai.ScrapeGalleryTorrentInfo(c.Request.Context(), client, s.upstreamBase(c.Request.Context(), uid), strconv.FormatInt(id, 10), token, gtid)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch torrent info failed: %v", err)})
		return
	}
	c.Header("Cache-Control", "no-store")
	httpx.JSON(c, http.StatusOK, result.Info)
}

// handleGalleryTorrentDownload proxies the .torrent file through the backend so
// the session cookie is used (the browser has none). It re-resolves the link by
// torrent id and never caches.
func (s *Server) handleGalleryTorrentDownload(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	gtid := strings.TrimSpace(c.Param("gtid"))
	if !gtidRe.MatchString(gtid) {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid torrent id"})
		return
	}
	idParam := strconv.FormatInt(id, 10)
	ctx := c.Request.Context()
	client, uid, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	base := s.upstreamBase(ctx, uid)

	torrents, err := exhentai.ScrapeGalleryTorrents(ctx, client, base, idParam, token)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery torrents failed: %v", err)})
		return
	}
	var torrent *model.GalleryTorrent
	for i := range torrents {
		if torrents[i].GTID == gtid {
			torrent = &torrents[i]
			break
		}
	}
	if torrent == nil {
		httpx.JSON(c, http.StatusNotFound, gin.H{"error": "torrent not found"})
		return
	}

	rawURL := torrent.DownloadURL
	if c.Query("variant") == "personalized" {
		result, infoErr := exhentai.ScrapeGalleryTorrentInfo(ctx, client, base, idParam, token, gtid)
		if infoErr != nil {
			httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch torrent info failed: %v", infoErr)})
			return
		}
		if result.PersonalizedURL == "" {
			httpx.JSON(c, http.StatusNotFound, gin.H{"error": "personalized torrent not available"})
			return
		}
		rawURL = result.PersonalizedURL
	}
	if rawURL == "" {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": "torrent download link missing"})
		return
	}

	data, err := exhentai.FetchTorrent(ctx, client, rawURL)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download torrent failed: %v", err)})
		return
	}

	filename := exhentai.TorrentFilename(torrent.Name, gtid)
	ascii := "torrent-" + gtid + ".torrent"
	c.Header("Content-Type", "application/x-bittorrent")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(filename)))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/x-bittorrent", data)
}

// ==================== Cache (read-through) endpoints ====================
//
// These are the read-through cache: a hit is served from gallery_cache, a miss
// fetches upstream and backfills. The online endpoints stay fresh and never
// write, so every cache write flows through here or a background refresher.
