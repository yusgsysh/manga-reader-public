package sync

import (
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
