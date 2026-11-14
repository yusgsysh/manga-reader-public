package sync

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/ident"
)

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestEnginePullsLiveChanges(t *testing.T) {
	serverClient := newTestClient(t)
	clientClient := newTestClient(t)
	serverSvc := newHostService(serverClient, "tok")
	ts := httptest.NewServer(newTestRouter(serverSvc))
	defer ts.Close()

	clientSvc := NewService(clientClient, Options{})
	enabled, url, token := true, ts.URL, "tok"
	if _, err := clientSvc.SaveClientConfig(t.Context(), ConfigUpdate{
		Enabled: &enabled, ServerURL: &url, Token: &token,
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go clientSvc.RunEngine(ctx)

	// Bootstrap finishes (server starts empty).
	waitFor(t, "bootstrap", func() bool {
		st, _ := clientSvc.LoadEngineState(t.Context())
		return st.Bootstrapped
	})

	now := time.Now().UTC()
	// Server -> client live push.
	seedProgress(t, serverClient, 100, "srv", 3, now)
	if err := serverSvc.RecordUpsert(t.Context(), testID(1), EntityReadingProgress, 100, "srv"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "server row pulled by client", func() bool {
		return getProgress(t, clientClient, 100, "srv") != nil
	})

	// Client -> server live push.
	seedProgress(t, clientClient, 200, "cli", 4, now)
	if err := clientSvc.RecordUpsert(t.Context(), testID(1), EntityReadingProgress, 200, "cli"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "client row pushed to server", func() bool {
		return getProgress(t, serverClient, 200, "cli") != nil
	})

	// Manual resync, then a fresh server row must reach the client.
	if err := clientSvc.RequestFullResync(t.Context()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "resync bootstrap", func() bool {
		st, _ := clientSvc.LoadEngineState(t.Context())
		return st.Bootstrapped
	})
	seedProgress(t, serverClient, 300, "srv2", 1, now.Add(time.Minute))
	if err := serverSvc.RecordUpsert(t.Context(), testID(1), EntityReadingProgress, 300, "srv2"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "post-resync server row pulled by client", func() bool {
		return getProgress(t, clientClient, 300, "srv2") != nil
	})

	// A server-side delete must reach the client.
	if _, err := serverClient.ReadingProgress.Delete().
		Where(readingprogress.GalleryID(100), readingprogress.Token("srv")).Exec(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := serverSvc.RecordDelete(t.Context(), testID(1), EntityReadingProgress, 100, "srv"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "server delete pulled by client", func() bool {
		return getProgress(t, clientClient, 100, "srv") == nil
	})
}

func TestFullResyncUsesNewerPeerRow(t *testing.T) {
	serverClient := newTestClient(t)
	clientClient := newTestClient(t)
	serverSvc := newHostService(serverClient, "tok")
	ts := httptest.NewServer(newTestRouter(serverSvc))
	defer ts.Close()

	old := time.Now().UTC().Add(-time.Hour)
	// Client has an older copy of 400; server has a newer one.
	seedProgress(t, clientClient, 400, "conf", 1, old)
	seedProgress(t, serverClient, 400, "conf", 9, old.Add(30*time.Minute))

	clientSvc := NewService(clientClient, Options{})
	enabled, url, token := true, ts.URL, "tok"
	if _, err := clientSvc.SaveClientConfig(t.Context(), ConfigUpdate{
		Enabled: &enabled, ServerURL: &url, Token: &token,
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go clientSvc.RunEngine(ctx)
	waitFor(t, "bootstrap", func() bool {
		st, _ := clientSvc.LoadEngineState(t.Context())
		return st.Bootstrapped
	})
	waitFor(t, "client pulls newer server row", func() bool {
		p := getProgress(t, clientClient, 400, "conf")
		return p != nil && p.CurrentPage == 9
	})
	if p := getProgress(t, serverClient, 400, "conf"); p == nil || p.CurrentPage != 9 {
		t.Fatalf("server row changed to %+v, want page 9", p)
	}
}

func TestEnginePullsBookshelfAndGalleryCache(t *testing.T) {
	serverClient := newTestClient(t)
	clientClient := newTestClient(t)
	serverSvc := newHostService(serverClient, "tok")
	ts := httptest.NewServer(newTestRouter(serverSvc))
	defer ts.Close()

	clientSvc := NewService(clientClient, Options{})
	enabled, url, token := true, ts.URL, "tok"
	if _, err := clientSvc.SaveClientConfig(t.Context(), ConfigUpdate{
		Enabled: &enabled, ServerURL: &url, Token: &token,
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go clientSvc.RunEngine(ctx)
	waitFor(t, "bootstrap", func() bool {
		st, _ := clientSvc.LoadEngineState(t.Context())
		return st.Bootstrapped
	})

	now := time.Now().UTC()
	// Server bookshelf -> client.
	if _, err := serverClient.Bookshelf.Create().
		SetUserID(testID(1)).SetGalleryID(500).SetToken("shelf").
		SetCreatedAt(now).SetUpdatedAt(now).Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := serverSvc.RecordUpsert(t.Context(), testID(1), EntityBookshelf, 500, "shelf"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "bookshelf pulled by client", func() bool {
		n, _ := clientClient.Bookshelf.Query().
			Where(bookshelf.GalleryID(500), bookshelf.Token("shelf")).Count(t.Context())
		return n == 1
	})

	// Server gallery cache -> client.
	seedGalleryCacheRow(t, serverClient, 600, "cache", now)
	if err := serverSvc.RecordUpsert(t.Context(), ident.Nil(), EntityGalleryCache, 600, "cache"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "gallery cache pulled by client", func() bool {
		return getGalleryCacheRow(t, clientClient, 600, "cache") != nil
	})
}
