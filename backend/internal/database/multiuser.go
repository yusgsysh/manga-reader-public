package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// The multi-user migration upgrades a database created by any earlier release:
//
//   - pre-auth single user (no tenant tables, no tenant_id columns),
//   - single-tenant with users (tenant_id columns, default tenant), or
//   - the multi-tenant release (tenant + users.tenant_id).
//
// Ownership policy (explicit, never random):
//
//   - Each old tenant is assigned to its oldest administrator, or its oldest
//     user when it has no administrator. All tenant-scoped rows move to that
//     account. This preserves the account that actually administered the
//     tenant and avoids spreading one tenant's data across accounts.
//   - A database with rows but no account at all leaves those rows with
//     user_id = 0 ("unowned"). They are never returned to a logged-in user;
//     an operator can assign them deliberately later. This is preferable to
//     inventing an account or exposing the data to whoever logs in first.
//   - Two old tenants mapping to the same account are checked for colliding
//     business keys; the migration aborts with a clear error instead of
//     silently dropping a row.
//   - Duplicate usernames across tenants cannot be represented by the new
//     global unique username. The migration aborts and lists them so an
//     operator can rename one account; guessing a new name is not allowed.
//
// The ownership columns are populated BEFORE Ent's structural migration, so
// the new (user_id, ...) unique indexes are built from correct values instead
// of colliding on the temporary default 0. The pre-step runs in one
// transaction; a failure leaves the old schema intact. Take a database backup
// before upgrading, and re-run the migration (it is idempotent) after fixing
// the reported conflict.
var userOwnedTables = []string{
	"bookshelf",
	"reading_progress",
	"prefill_job",
	"tag_preference",
	"tag_sync_baseline",
	"sync_change",
}

// prepareMultiUserMigration must run before client.Schema.Create.
func prepareMultiUserMigration(ctx context.Context, conn *sql.DB, dialectName string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin multi-user migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	usersExists, err := tableExists(ctx, tx, dialectName, "users")
	if err != nil {
		return err
	}
	if usersExists {
		dup, err := duplicateUsernames(ctx, tx)
		if err != nil {
			return err
		}
		if len(dup) > 0 {
			return fmt.Errorf("cannot migrate to multi-user: usernames are no longer tenant-scoped but these are duplicated: %s; rename one account per name and retry", strings.Join(dup, ", "))
		}
	}

	tenantsExists, err := tableExists(ctx, tx, dialectName, "tenant")
	if err != nil {
		return err
	}
	tenantCol, err := columnExists(ctx, tx, dialectName, "users", "tenant_id")
	if err != nil {
		return err
	}

	// ownerByTenant maps an old tenant id to its new owner user id.
	ownerByTenant := make(map[int64]int64)
	defaultOwner := int64(0)
	if usersExists {
		defaultOwner, err = firstOwner(ctx, tx, dialectName)
		if err != nil {
			return err
		}
	}
	if tenantsExists {
		tenants, err := tenantIDs(ctx, tx)
		if err != nil {
			return err
		}
		for _, tenantID := range tenants {
			owner := int64(0)
			if usersExists && tenantCol {
				owner, err = firstOwnerOfTenant(ctx, tx, dialectName, tenantID)
				if err != nil {
					return err
				}
			}
			// A tenant with no account keeps its rows unowned (0); never
			// silently hand them to another tenant's administrator.
			ownerByTenant[tenantID] = owner
		}
	}

	for _, table := range userOwnedTables {
		exists, err := tableExists(ctx, tx, dialectName, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		hasUserID, err := columnExists(ctx, tx, dialectName, table, "user_id")
		if err != nil {
			return err
		}
		hasTenantID, err := columnExists(ctx, tx, dialectName, table, "tenant_id")
		if err != nil {
			return err
		}
		if !hasUserID {
			columnType := "INTEGER"
			if isPostgres(dialectName) {
				columnType = "BIGINT"
			}
			stmt := fmt.Sprintf(`ALTER TABLE "%s" ADD COLUMN "user_id" %s NOT NULL DEFAULT 0`, table, columnType)
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("add user_id to %s: %w", table, err)
			}
		}

		// Only assign ownership when this is a legacy table (the column was
		// just added or old tenant_id still exists). A table that already has
		// user_id is either fully migrated or deliberately holds unowned
		// rows; re-running startup must never silently claim those rows for
		// the first administrator.
		switch {
		case hasTenantID && len(ownerByTenant) > 0:
			for tenantID, owner := range ownerByTenant {
				stmt := fmt.Sprintf(`UPDATE "%s" SET "user_id" = %s WHERE "tenant_id" = %s AND "user_id" = 0`, table, placeholder(dialectName, 1), placeholder(dialectName, 2))
				if _, err := tx.ExecContext(ctx, stmt, owner, tenantID); err != nil {
					return fmt.Errorf("assign %s rows of tenant %d: %w", table, tenantID, err)
				}
			}
		case !hasUserID && defaultOwner > 0:
			stmt := fmt.Sprintf(`UPDATE "%s" SET "user_id" = %s WHERE "user_id" = 0`, table, placeholder(dialectName, 1))
			if _, err := tx.ExecContext(ctx, stmt, defaultOwner); err != nil {
				return fmt.Errorf("assign %s rows to the legacy owner: %w", table, err)
			}
		}
	}

	// Nodes were per tenant. Keep the data but make names globally unique by
	// suffixing every duplicate after the oldest row.
	if err := dedupeNodeNames(ctx, tx, dialectName); err != nil {
		return err
	}

	if err := detectOwnedKeyCollisions(ctx, tx, dialectName); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit multi-user migration: %w", err)
	}
	return nil
}

// finishMultiUserMigration runs after Ent's structural migration. It normalises
// roles and removes the obsolete tenant table.
func finishMultiUserMigration(ctx context.Context, conn *sql.DB, dialectName string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin multi-user cleanup: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	usersExists, err := tableExists(ctx, tx, dialectName, "users")
	if err != nil {
		return err
	}
	if usersExists {
		if _, err := tx.ExecContext(ctx, `UPDATE "users" SET "role" = 'admin' WHERE "role" IN ('platform_admin', 'tenant_admin')`); err != nil {
			return fmt.Errorf("normalise administrator roles: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE "users" SET "role" = 'user' WHERE "role" NOT IN ('admin', 'user')`); err != nil {
			return fmt.Errorf("normalise user roles: %w", err)
		}
	}

	tenantsExists, err := tableExists(ctx, tx, dialectName, "tenant")
	if err != nil {
		return err
	}
	if tenantsExists {
		if _, err := tx.ExecContext(ctx, `DROP TABLE "tenant"`); err != nil {
			return fmt.Errorf("drop legacy tenant table: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit multi-user cleanup: %w", err)
	}
	return nil
}

func isPostgres(dialectName string) bool { return strings.EqualFold(dialectName, "postgres") }

func placeholder(dialectName string, n int) string {
	if isPostgres(dialectName) {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func tableExists(ctx context.Context, tx *sql.Tx, dialectName, table string) (bool, error) {
	var query string
	var args []any
	if isPostgres(dialectName) {
		query = `SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = $1`
		args = []any{table}
	} else {
		query = `SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = ?`
		args = []any{table}
	}
	var one int
	err := tx.QueryRowContext(ctx, query, args...).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return true, nil
}

func columnExists(ctx context.Context, tx *sql.Tx, dialectName, table, column string) (bool, error) {
	if isPostgres(dialectName) {
		var one int
		err := tx.QueryRowContext(ctx,
			`SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
			table, column).Scan(&one)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("check column %s.%s: %w", table, column, err)
		}
		return true, nil
	}

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info("%s")`, table))
	if err != nil {
		return false, fmt.Errorf("check column %s.%s: %w", table, column, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

func duplicateUsernames(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT "username" FROM "users" GROUP BY "username" HAVING COUNT(*) > 1 ORDER BY "username"`)
	if err != nil {
		return nil, fmt.Errorf("check duplicate usernames: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func firstOwner(ctx context.Context, tx *sql.Tx, dialectName string) (int64, error) {
	// After the id->uuid migration users.id is a UUID string; there is no
	// integer legacy owner to return, which is fine because the ownership
	// assignment below only runs for tables that still lack a user_id column.
	var raw string
	err := tx.QueryRowContext(ctx,
		`SELECT "id" FROM "users" ORDER BY CASE WHEN "role" IN ('platform_admin', 'tenant_admin', 'admin') THEN 0 ELSE 1 END, "id" LIMIT 1`).Scan(&raw)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("select legacy owner: %w", err)
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, nil
	}
	return id, nil
}

func firstOwnerOfTenant(ctx context.Context, tx *sql.Tx, dialectName string, tenantID int64) (int64, error) {
	query := `SELECT "id" FROM "users" WHERE "tenant_id" = ` + placeholder(dialectName, 1) +
		` ORDER BY CASE WHEN "role" IN ('platform_admin', 'tenant_admin', 'admin') THEN 0 ELSE 1 END, "id" LIMIT 1`
	var id int64
	err := tx.QueryRowContext(ctx, query, tenantID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("select owner of tenant %d: %w", tenantID, err)
	}
	return id, nil
}

func tenantIDs(ctx context.Context, tx *sql.Tx) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT "id" FROM "tenant" ORDER BY "id"`)
	if err != nil {
		return nil, fmt.Errorf("read tenants: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func dedupeNodeNames(ctx context.Context, tx *sql.Tx, dialectName string) error {
	exists, err := tableExists(ctx, tx, dialectName, "node")
	if err != nil || !exists {
		return err
	}
	hasTenant, err := columnExists(ctx, tx, dialectName, "node", "tenant_id")
	if err != nil || !hasTenant {
		return err
	}
	// Rename every row that shares a name with a lower-id row. The suffix is
	// stable and only affects the display name.
	query := `UPDATE "node" SET "name" = "name" || ' (tenant ' || "tenant_id" || ')' WHERE "id" IN (
		SELECT "id" FROM "node" n WHERE EXISTS (
			SELECT 1 FROM "node" m WHERE m."name" = n."name" AND m."id" < n."id"
		)
	)`
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("dedupe node names: %w", err)
	}
	return nil
}

func detectOwnedKeyCollisions(ctx context.Context, tx *sql.Tx, dialectName string) error {
	checks := []struct {
		table   string
		columns []string
	}{
		{"bookshelf", []string{"user_id", "gallery_id", "token"}},
		{"reading_progress", []string{"user_id", "gallery_id", "token"}},
		{"tag_preference", []string{"user_id", "tag"}},
		{"tag_sync_baseline", []string{"user_id", "tag"}},
	}
	for _, check := range checks {
		exists, err := tableExists(ctx, tx, dialectName, check.table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		quoted := make([]string, len(check.columns))
		for i, c := range check.columns {
			quoted[i] = fmt.Sprintf(`"%s"`, c)
		}
		query := fmt.Sprintf(`SELECT COUNT(*) FROM (SELECT %s FROM "%s" GROUP BY %s HAVING COUNT(*) > 1) dup`,
			strings.Join(quoted, ", "), check.table, strings.Join(quoted, ", "))
		var count int
		if err := tx.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return fmt.Errorf("check %s ownership collisions: %w", check.table, err)
		}
		if count > 0 {
			return fmt.Errorf("cannot migrate to multi-user: %s has %d rows that would collide after assigning them to one owner; review the old tenant assignments and retry", check.table, count)
		}
	}
	return nil
}

// logUnownedRows reports rows left with the zero (unowned) owner UUID after the
// migration, so an operator can assign them deliberately instead of assuming
// they were lost.
func logUnownedRows(ctx context.Context, conn *sql.DB) {
	for _, table := range userOwnedTables {
		var count int
		if err := conn.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM "%s" WHERE "user_id" = '%s'`, table, zeroUUID)).Scan(&count); err != nil {
			continue
		}
		if count > 0 {
			slog.Warn("multi-user migration left unowned rows; they are hidden until an operator assigns a user",
				"table", table, "rows", count)
		}
	}
}
