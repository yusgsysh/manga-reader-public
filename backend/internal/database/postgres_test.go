package database

import (
	"os"
	"strconv"
	"testing"
	"time"

	"manga-reader/internal/config"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/tenant"
)

// TestPostgresCompatibility is opt-in because a PostgreSQL server is not part
// of the default test environment. Point MANGA_READER_TEST_POSTGRES_DSN at a
// disposable database to verify the same schema/migration/query paths that
// SQLite covers by default.
func TestPostgresCompatibility(t *testing.T) {
	dsn := os.Getenv("MANGA_READER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set MANGA_READER_TEST_POSTGRES_DSN to run PostgreSQL compatibility tests")
	}
	db, err := NewDB(Options{Driver: DriverPostgres, DSN: dsn})
	if err != nil {
		t.Fatalf("NewDB postgres: %v", err)
	}
	defer db.Close()
	ctx := t.Context()

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	tenantRow, err := db.Client.Tenant.Create().
		SetName("pg-test-" + suffix).
		SetSlug("pg-test-" + suffix).
		SetStatus(config.StatusActive).Save(ctx)
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if _, err := db.Client.Bookshelf.Create().
		SetTenantID(int64(tenantRow.ID)).SetGalleryID(1).SetToken("pg").Save(ctx); err != nil {
		t.Fatalf("create bookshelf: %v", err)
	}
	rows, err := db.Client.Bookshelf.Query().
		Where(bookshelf.TenantIDEQ(int64(tenantRow.ID)), bookshelf.GalleryID(1)).
		All(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("tenant query = %+v (err %v), want one row", rows, err)
	}

	// The multi-tenancy migration is idempotent on the PostgreSQL backend too:
	// reopening against the same DSN must not duplicate the default tenant.
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	again, err := NewDB(Options{Driver: DriverPostgres, DSN: dsn})
	if err != nil {
		t.Fatalf("reopen postgres: %v", err)
	}
	defer again.Close()
	count, err := again.Client.Tenant.Query().
		Where(tenant.SlugEQ(config.DefaultTenantSlug)).Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("default tenant count = %d (err %v), want 1", count, err)
	}
}
