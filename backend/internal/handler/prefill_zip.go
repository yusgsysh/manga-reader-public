package handler

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"manga-reader/internal/archive"
	"manga-reader/internal/ent"
	"manga-reader/internal/httpx"
	"manga-reader/internal/ident"
	"manga-reader/internal/metrics"
	"manga-reader/internal/storage"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// handlePrefillZip streams a ZIP archive for a job. When the job has a stored
// archive object it is served straight from the archive store; otherwise the
// ZIP is generated on the fly (from the image cache, fetching misses upstream,
// or straight from upstream when the cache is disabled).
func (s *Server) handlePrefillZip(c *gin.Context) {
	// ZIP download streams for as long as the client reads it; skip latency.
	metrics.MarkLongRunning(c)
	if s.DB == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil && s.Archive == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no storage configured"})
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	row, err := s.getPrefillJob(c.Request.Context(), uid, id)
	if err != nil {
		if ent.IsNotFound(err) {
			httpx.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill get failed", "job_id", id, "error", err)
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}

	ctx := c.Request.Context()
	name := prefillZipFilename(row.Title)

	// A stored archive is the fast path: redirect to a signed URL (when presign
	// is enabled) or stream it back at constant memory.
	if s.Archive != nil && row.ArchiveKey != nil {
		if s.ArchivePresign {
			if loc, presignErr := s.Archive.PresignGet(ctx, *row.ArchiveKey, s.ArchivePresignExpiry, presignZipParams(name)); presignErr == nil {
				c.Redirect(http.StatusFound, loc)
				return
			} else {
				slog.Error("prefill archive presign failed; proxying instead", "job_id", id, "error", presignErr)
			}
		}
		rc, _, size, getErr := s.Archive.GetStream(ctx, *row.ArchiveKey)
		if getErr == nil {
			defer rc.Close()
			writeZipHeaders(c, name)
			c.Header("Content-Length", strconv.FormatInt(size, 10))
			c.Status(http.StatusOK)
			if _, err := io.Copy(c.Writer, rc); err != nil {
				slog.Error("prefill archive stream failed", "job_id", id, "error", err)
			}
			return
		}
		if !storage.IsNotFound(getErr) {
			slog.Error("prefill archive read failed; rebuilding on the fly", "job_id", id, "key", *row.ArchiveKey, "error", getErr)
		}
	}

	zw := zip.NewWriter(c.Writer)
	started := false
	missing := []string{}

	for i, pageURL := range row.Urls {
		if ctx.Err() != nil {
			break
		}

		pageCtx, pageCancel := context.WithTimeout(ctx, prefillPageTimeout)
		data, contentType, err := s.fetchPage(pageCtx, uid, pageURL)
		pageCancel()
		if err != nil {
			slog.Error("prefill zip fetch failed", "index", i, "error", err)
			missing = append(missing, fmt.Sprintf("%d: %s", i+1, err.Error()))
			continue
		}

		if !started {
			writeZipHeaders(c, name)
			c.Status(http.StatusOK)
			started = true
		}

		entryName := fmt.Sprintf("%03d%s", i+1, prefillImageExt(contentType, pageURL))
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Store})
		if err != nil {
			slog.Error("prefill zip entry failed", "index", i, "error", err)
			break
		}
		if _, err := w.Write(data); err != nil {
			slog.Error("prefill zip write failed", "index", i, "error", err)
			break
		}
		c.Writer.Flush()
	}

	if !started {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no pages could be fetched for ZIP"})
		return
	}

	if len(missing) > 0 {
		missingData := []byte(strings.Join(missing, "\n"))
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "_missing.txt", Method: zip.Store})
		if err == nil {
			// Best effort: the ZIP is already streaming to the client and a
			// failed trailing entry cannot be reported back anymore.
			w.Write(missingData) //nolint:errcheck // request already committed
		}
	}

	if err := zw.Close(); err != nil {
		slog.Error("prefill zip close failed", "error", err)
	}
}

// writeZipHeaders sets the response headers shared by the archive and on-the-fly
// ZIP paths.
func writeZipHeaders(c *gin.Context, name string) {
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", zipContentDisposition(name))
	c.Header("Cache-Control", "no-store")
}

// zipContentDisposition renders the attachment filename header value (ASCII
// fallback + RFC 5987 encoded form).
func zipContentDisposition(name string) string {
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		asciiFallback(name), rfc5987Escape(name))
}

// presignZipParams returns the extra query parameters for a signed archive URL,
// so the object store sends the same Content-Disposition the proxy path would.
func presignZipParams(name string) map[string]string {
	return map[string]string{"response-content-disposition": zipContentDisposition(name)}
}

// handlePrefillZipHead handles HEAD requests for the ZIP endpoint.
// It returns 200 if the job exists and storage is configured, without streaming any data.
func (s *Server) handlePrefillZipHead(c *gin.Context) {
	if s.DB == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil && s.Archive == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no storage configured"})
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	_, err := s.getPrefillJob(c.Request.Context(), uid, id)
	if err != nil {
		if ent.IsNotFound(err) {
			httpx.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill zip head get failed", "job_id", id, "error", err)
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	c.Status(http.StatusOK)
}

// ==================== Helpers ====================

func prefillImageExt(contentType, pageURL string) string {
	return archive.ExtForContentType(contentType, pageURL)
}

// prefillArchiveKey names a stored ZIP archive object. The final path segment
// keeps the human-readable gallery title (sanitized) as the object name, while
// the user ID and job ID live in the path so multi-user deployments stay
// isolated and two jobs that share a title never collide.
func prefillArchiveKey(userID, jobID ident.UUID, title string) string {
	return userID.String() + "/" + jobID.String() + "/" + prefillZipFilename(title) + ".zip"
}

// prefillZipFilename mirrors the frontend's sanitizeFilename so archives are
// named consistently with the previous client-side ZIP downloads. It also
// strips "/" so the result is safe as a single object-key path segment.
func prefillZipFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == '"' || r == '\\' || strings.ContainsRune(`<>:|?*/`, r):
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	runes := []rune(strings.Join(strings.Fields(b.String()), " "))
	if len(runes) > 100 {
		runes = []rune(strings.TrimSpace(string(runes[:100])))
	}
	name := string(runes)
	if name == "" {
		name = "download"
	}
	return name
}

func asciiFallback(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 0x20 && r < 0x7f && r != '"' && r != '\\' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "download"
	}
	return b.String()
}

// rfc5987Escape percent-encodes a string for the filename* parameter.
func rfc5987Escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if isAttrChar(ch) {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
