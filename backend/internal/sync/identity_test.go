package sync

import (
	"net/http/httptest"
	"testing"
	"time"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/ident"
)

func seedUser(t *testing.T, client *ent.Client, username string) ident.UUID {
	t.Helper()
	u, err := client.User.Create().
		SetUsername(username).
		SetPasswordHash("not-a-real-hash").
		Save(t.Context())
	if err != nil {
		t.Fatalf("seed user %q: %v", username, err)
	}
	return u.ID
}

func seedProgressFor(t *testing.T, client *ent.Client, userID ident.UUID, galleryID int64, token string, page int, updatedAt time.Time) {
	t.Helper()
	_, err := client.ReadingProgress.Create().
		SetUserID(userID).
		SetGalleryID(galleryID).
		SetToken(token).
		SetCurrentPage(page).
		SetProgress(float64(page) / 10).
		SetCreatedAt(updatedAt).
		SetUpdatedAt(updatedAt).
		Save(t.Context())
	if err != nil {
		t.Fatalf("seed progress: %v", err)
	}
}

func getProgressForUser(t *testing.T, client *ent.Client, userID ident.UUID, galleryID int64, token string) *ent.ReadingProgress {
	t.Helper()
	p, err := client.ReadingProgress.Query().
		Where(
			readingprogress.UserIDEQ(userID),
			readingprogress.GalleryID(galleryID),
			readingprogress.Token(token),
		).
		Only(t.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		t.Fatalf("get progress: %v", err)
	}
	return p
}

// Accounts are created per instance with random UUIDs; the same person has a
// different user_id on each side. Changes must land on the local account with
// the same username instead of a ghost owner nobody can read.
func TestSyncMapsAccountsByUsername(t *testing.T) {
	serverClient := newTestClient(t)
	clientClient := newTestClient(t)
	serverUser := seedUser(t, serverClient, "admin")
	clientUser := seedUser(t, clientClient, "admin")
	if serverUser == clientUser {
		t.Fatal("test requires distinct instance-local user ids")
	}

	serverSvc := newHostService(serverClient, "tok")
	ts := httptest.NewServer(newTestRouter(serverSvc))
	defer ts.Close()
	clientSvc := NewService(clientClient, Options{})
	cfg := ClientConfig{Enabled: true, ServerURL: ts.URL, Token: "tok"}
	now := time.Now().UTC()

	// Push: the server must store the row under its own "admin".
	seedProgressFor(t, clientClient, clientUser, 1, "push", 4, now)
	if err := clientSvc.RecordUpsert(t.Context(), clientUser, EntityReadingProgress, 1, "push"); err != nil {
		t.Fatalf("record: %v", err)
	}
	clientSvc.engine.pushCycle(t.Context(), cfg)
	if p := getProgressForUser(t, serverClient, serverUser, 1, "push"); p == nil || p.CurrentPage != 4 {
		t.Fatalf("server row under its own user = %+v, want page 4", p)
	}
	if p := getProgressForUser(t, serverClient, clientUser, 1, "push"); p != nil {
		t.Fatalf("server stored a ghost-owner row: %+v", p)
	}

	// Pull: the client must store the row under its own "admin".
	seedProgressFor(t, serverClient, serverUser, 2, "pull", 6, now.Add(time.Minute))
	if err := serverSvc.RecordUpsert(t.Context(), serverUser, EntityReadingProgress, 2, "pull"); err != nil {
		t.Fatalf("record: %v", err)
	}
	clientSvc.engine.pushCycle(t.Context(), cfg)
	if p := getProgressForUser(t, clientClient, clientUser, 2, "pull"); p == nil || p.CurrentPage != 6 {
		t.Fatalf("client row under its own user = %+v, want page 6", p)
	}
	if p := getProgressForUser(t, clientClient, serverUser, 2, "pull"); p != nil {
		t.Fatalf("client stored a ghost-owner row: %+v", p)
	}
}

// A username the receiver does not have cannot be attributed when several
// accounts exist; such a change is skipped instead of leaking into a random
// account.
func TestApplyChangesSkipsUnknownUser(t *testing.T) {
	client := newTestClient(t)
	seedUser(t, client, "alice")
	seedUser(t, client, "bob")
	now := time.Now().UTC()

	ch := Change{
		UserID: testID(9), Username: "carol",
		Entity: EntityReadingProgress, GalleryID: 1, Token: "tok", Op: OpUpsert,
		Row: &Row{CurrentPage: 2, Progress: 0.2, CreatedAt: now, UpdatedAt: now},
	}
	applied, skipped, err := ApplyChanges(t.Context(), client, []Change{ch})
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if applied != 0 || skipped != 1 {
		t.Fatalf("apply = %d/%d, want 0/1", applied, skipped)
	}
	if n, _ := client.ReadingProgress.Query().Count(t.Context()); n != 0 {
		t.Fatalf("rows = %d, want none", n)
	}
}
