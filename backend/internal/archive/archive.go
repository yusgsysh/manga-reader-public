// Package archive builds a ZIP of a gallery's page images as a stream, so the
// archive is never materialized on disk or in memory. It pairs an in-process
// producer (fetch pages → zip.Writer → io.Pipe) with an object-store consumer
// (multipart upload), giving ordered, back-pressured, cancellable generation.
package archive

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"

	"manga-reader/internal/storage"
)

// Fetch retrieves one page's raw bytes and its content type. The bytes are kept
// verbatim: no decode/re-encode happens, so the ZIP preserves the source
// images exactly.
type Fetch func(ctx context.Context, pageURL string) (data []byte, contentType string, err error)

// Options tunes ZIP generation.
type Options struct {
	// Concurrency bounds how many pages are fetched in parallel. Entry order in
	// the ZIP is always preserved regardless of concurrency. <=1 means serial.
	Concurrency int
	// NameEntry returns the ZIP entry name for a page. Default: "%03d<ext>".
	NameEntry func(index int, pageURL string, contentType string) string
	// MissingName is the name of a trailing manifest listing failed pages.
	// Empty disables the manifest.
	MissingName string
	// OnMissing is called for each page that failed to fetch (optional).
	OnMissing func(index int, pageURL string, err error)
}

// Result reports how many pages could not be included.
type Result struct {
	Missing int
}

// Build writes an ordered ZIP of pages into dst. Per-page fetch failures are
// recorded (via OnMissing and the manifest) rather than aborting the archive;
// only hard failures — context cancellation or a broken dst — return an error,
// in which case the partially written stream must be discarded by the caller.
func Build(ctx context.Context, dst io.Writer, pages []string, fetch Fetch, opts Options) (Result, error) {
	conc := opts.Concurrency
	if conc < 1 {
		conc = 1
	}
	if conc > len(pages) {
		conc = len(pages)
	}
	nameEntry := opts.NameEntry
	if nameEntry == nil {
		nameEntry = defaultNameEntry
	}

	// buildCtx lets a broken writer cancel the in-flight fetchers even when the
	// parent context is still live.
	buildCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	zw := zip.NewWriter(dst)

	type item struct {
		index       int
		data        []byte
		contentType string
		err         error
	}

	results := make(chan item, conc)
	var next atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < conc; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idx := int(next.Add(1)) - 1
				if idx >= len(pages) {
					return
				}
				data, ct, err := fetch(buildCtx, pages[idx])
				select {
				case results <- item{index: idx, data: data, contentType: ct, err: err}:
				case <-buildCtx.Done():
					return
				}
			}
		}()
	}
	go func() { wg.Wait(); close(results) }()

	var (
		missing      int
		missingLines []string
		nextToWrite  int
		pending      = make(map[int]item)
		buildErr     error
	)

loop:
	for {
		select {
		case <-buildCtx.Done():
			buildErr = buildCtx.Err()
			break loop
		case res, ok := <-results:
			if !ok {
				break loop
			}
			pending[res.index] = res
			for {
				r, present := pending[nextToWrite]
				if !present {
					break
				}
				delete(pending, nextToWrite)
				if r.err != nil {
					missing++
					missingLines = append(missingLines, fmt.Sprintf("%d: %s", nextToWrite+1, r.err.Error()))
					if opts.OnMissing != nil {
						opts.OnMissing(nextToWrite, pages[nextToWrite], r.err)
					}
				} else if err := writeEntry(zw, nameEntry(nextToWrite, pages[nextToWrite], r.contentType), r.data); err != nil {
					buildErr = err
					cancel()
					break loop
				}
				nextToWrite++
			}
		}
	}

	if buildErr == nil && nextToWrite < len(pages) {
		// Workers exited early (parent context cancelled) before every page was
		// written; report cancellation rather than a silently truncated archive.
		if err := buildCtx.Err(); err != nil {
			buildErr = err
		} else {
			buildErr = fmt.Errorf("archive stopped before all pages were written")
		}
	}

	if buildErr != nil {
		_ = zw.Close() // discard: the reader sees the error via the pipe
		return Result{Missing: missing}, buildErr
	}

	if missing > 0 && opts.MissingName != "" {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: opts.MissingName, Method: zip.Store})
		if err != nil {
			return Result{Missing: missing}, err
		}
		if _, err := io.WriteString(w, strings.Join(missingLines, "\n")); err != nil {
			return Result{Missing: missing}, err
		}
	}

	if err := zw.Close(); err != nil {
		return Result{Missing: missing}, err
	}
	return Result{Missing: missing}, nil
}

// BuildAndUpload streams a ZIP straight into object storage: a producer
// goroutine builds the archive into one end of an io.Pipe while the upload
// reads the other, so no complete ZIP ever exists in memory or on disk. The
// returned ObjectInfo describes the stored object only when the whole archive
// completed successfully; any failure aborts the (multipart) upload so a
// half-written object is never left visible.
func BuildAndUpload(ctx context.Context, st storage.ObjectStorage, key string, pages []string, fetch Fetch, opts Options) (storage.ObjectInfo, Result, error) {
	buildCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	pr, pw := io.Pipe()
	type outcome struct {
		res Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := Build(buildCtx, pw, pages, fetch, opts)
		if err != nil {
			_ = pw.CloseWithError(err)
		} else {
			_ = pw.Close()
		}
		done <- outcome{res: res, err: err}
	}()

	info, uploadErr := st.Upload(ctx, key, pr, -1, "application/zip")
	if uploadErr != nil {
		// Stop the producer and unblock any Write currently waiting on the pipe.
		cancel()
		_ = pr.Close()
	}
	out := <-done
	_ = pr.Close()

	if uploadErr != nil {
		return storage.ObjectInfo{}, Result{}, uploadErr
	}
	if out.err != nil {
		return storage.ObjectInfo{}, Result{}, out.err
	}
	return info, out.res, nil
}

func writeEntry(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ExtForContentType picks a stable file extension for a page, preferring the
// content type and falling back to the URL's path extension.
func ExtForContentType(contentType, pageURL string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch ct {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	if u, err := url.Parse(pageURL); err == nil {
		switch strings.ToLower(path.Ext(u.Path)) {
		case ".jpg", ".jpeg":
			return ".jpg"
		case ".png":
			return ".png"
		case ".webp":
			return ".webp"
		case ".gif":
			return ".gif"
		}
	}
	return ".jpg"
}

func defaultNameEntry(index int, pageURL, contentType string) string {
	return fmt.Sprintf("%03d%s", index+1, ExtForContentType(contentType, pageURL))
}
