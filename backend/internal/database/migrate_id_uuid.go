package database

import (
	"context"
	"database/sql"
	"fmt"

	"manga-reader/internal/ident"
)

// zeroUUID is the "unowned" owner sentinel, the UUID analogue of the previous
// user_id = 0 that the multi-user migration left for rows with no account.
const zeroUUID = "00000000-0000-0000-0000-000000000000"

// idDialect holds the SQL column type names ent emits for each field kind, so a
// hand-written table rebuild reproduces the exact target schema Atlas would
// otherwise generate (matching types keeps Atlas's later diff a no-op).
type idDialect struct {
	uuid      string
	integer   string
	text      string
	boolean   string
	real      string
	timestamp string
	json      string
}

func idDialectFor(dialectName string) idDialect {
	if isPostgres(dialectName) {
		return idDialect{
			uuid: "bytea", integer: "bigint", text: "varchar", boolean: "boolean",
			real: "double precision", timestamp: "timestamp with time zone", json: "jsonb",
		}
	}
	return idDialect{
		uuid: "blob", integer: "integer", text: "text", boolean: "bool",
		real: "real", timestamp: "datetime", json: "json",
	}
}

// zeroUUIDExpr returns the SQL literal for the 16 zero bytes that mark an
// unowned row, typed for the target column.
func (d idDialect) zeroUUIDExpr() string {
	if d.uuid == "bytea" {
		return `'\x00000000000000000000000000000000'::bytea`
	}
	return `x'00000000000000000000000000000000'`
}

// migrateIDsToUUID converts every auto-increment integer identity column to a
// UUID. Auto-increment ids collide across instances when databases are merged,
// dumped/restored or otherwise migrated, so the schema avoids them; this runs
// once on an existing database and remaps the old integer ids (users.id and the
// user_id references, plus node/node_api_key ids) onto freshly generated UUIDs.
//
// It runs after prepareMultiUserMigration (so user_id is already assigned) and
// before Ent's structural migration. After it, the schema already matches the
// target, so Ent's Schema.Create is effectively a no-op. It is idempotent: the
// integer-typed "users"."id" column is the sentinel — once it is a UUID the
// migration is skipped.
func migrateIDsToUUID(ctx context.Context, conn *sql.DB, dialectName string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin id->uuid migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	d := idDialectFor(dialectName)

	// Which tables still carry an integer identity that must become a UUID?
	migrateUsers, err := tableColumnIsInteger(ctx, tx, dialectName, "users", "id")
	if err != nil {
		return err
	}
	migrateNode, err := tableColumnIsInteger(ctx, tx, dialectName, "node", "id")
	if err != nil {
		return err
	}
	migrateNodeKey, err := tableColumnIsInteger(ctx, tx, dialectName, "node_api_key", "id")
	if err != nil {
		return err
	}

	// user-owned tables are split between those whose primary key changes
	// (full rebuild) and those that only remap a non-key user_id column.
	rebuildChildren := []string{"bookshelf", "reading_progress", "user_setting", "tag_preference", "tag_sync_baseline"}
	alterChildren := []string{"prefill_job", "sync_change"}
	var migrateChild []string
	for _, t := range append(append([]string{}, rebuildChildren...), alterChildren...) {
		isInt, err := tableColumnIsInteger(ctx, tx, dialectName, t, "user_id")
		if err != nil {
			return err
		}
		if isInt {
			migrateChild = append(migrateChild, t)
		}
	}

	anyUserMigration := migrateUsers || len(migrateChild) > 0
	if !anyUserMigration && !migrateNode && !migrateNodeKey {
		return nil // already migrated, or nothing to migrate.
	}

	var userMap map[int64][]byte
	if migrateUsers {
		userMap, err = readIDMap(ctx, tx, dialectName, "users")
		if err != nil {
			return err
		}
	} else {
		userMap = map[int64][]byte{}
	}
	var nodeMap map[int64][]byte
	if migrateNode {
		nodeMap, err = readIDMap(ctx, tx, dialectName, "node")
		if err != nil {
			return err
		}
	}
	var keyMap map[int64][]byte
	if migrateNodeKey {
		keyMap, err = readIDMap(ctx, tx, dialectName, "node_api_key")
		if err != nil {
			return err
		}
	}

	if anyUserMigration {
		if err := createIDMap(ctx, tx, dialectName, d, "__mr_user_id_map", userMap); err != nil {
			return err
		}
	}
	if migrateNode {
		if err := createIDMap(ctx, tx, dialectName, d, "__mr_node_id_map", nodeMap); err != nil {
			return err
		}
	}
	if migrateNodeKey {
		if err := createIDMap(ctx, tx, dialectName, d, "__mr_key_id_map", keyMap); err != nil {
			return err
		}
	}

	// Bookshelf and reading_progress keep a UUID single-column id; the old
	// auto-increment value is meaningless, so each row gets a fresh UUID.
	for _, t := range []string{"bookshelf", "reading_progress"} {
		if !contains(migrateChild, t) {
			continue
		}
		idMap, err := readIDMap(ctx, tx, dialectName, t)
		if err != nil {
			return err
		}
		if err := createIDMap(ctx, tx, dialectName, d, "__mr_"+t+"_id_map", idMap); err != nil {
			return err
		}
	}

	if migrateUsers {
		if err := rebuildUsers(ctx, tx, d); err != nil {
			return err
		}
	}
	if migrateNode {
		if err := rebuildNode(ctx, tx, d); err != nil {
			return err
		}
	}
	if migrateNodeKey {
		if err := rebuildNodeAPIKey(ctx, tx, d); err != nil {
			return err
		}
	}

	for _, t := range migrateChild {
		switch t {
		case "bookshelf":
			err = rebuildBookshelf(ctx, tx, d)
		case "reading_progress":
			err = rebuildReadingProgress(ctx, tx, d)
		case "user_setting":
			err = rebuildUserSetting(ctx, tx, d)
		case "tag_preference":
			err = rebuildTagPreference(ctx, tx, d)
		case "tag_sync_baseline":
			err = rebuildTagSyncBaseline(ctx, tx, d)
		case "prefill_job":
			err = convertUserIDColumn(ctx, tx, dialectName, d, "prefill_job", []idIndex{
				{name: "prefilljob_user_id_status_created_at", columns: []string{"user_id", "status", "created_at"}},
				{name: "prefilljob_user_id_gallery_id_token", columns: []string{"user_id", "gallery_id", "token"}},
			})
		case "sync_change":
			err = convertUserIDColumn(ctx, tx, dialectName, d, "sync_change", []idIndex{
				{name: "syncchange_user_id_entity_gallery_id_token", columns: []string{"user_id", "entity", "gallery_id", "token"}},
			})
		}
		if err != nil {
			return err
		}
	}

	if anyUserMigration {
		if _, err := tx.ExecContext(ctx, `DROP TABLE "__mr_user_id_map"`); err != nil {
			return err
		}
	}
	if migrateNode {
		if _, err := tx.ExecContext(ctx, `DROP TABLE "__mr_node_id_map"`); err != nil {
			return err
		}
	}
	if migrateNodeKey {
		if _, err := tx.ExecContext(ctx, `DROP TABLE "__mr_key_id_map"`); err != nil {
			return err
		}
	}
	for _, t := range []string{"bookshelf", "reading_progress"} {
		if !contains(migrateChild, t) {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE "__mr_%s_id_map"`, t)); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit id->uuid migration: %w", err)
	}
	return nil
}

// tableColumnIsInteger reports whether a table exists and has an integer-typed
// identity column (the pre-UUID sentinel). A missing table or column is simply
// "not integer".
func tableColumnIsInteger(ctx context.Context, tx *sql.Tx, dialectName, table, column string) (bool, error) {
	exists, err := tableExists(ctx, tx, dialectName, table)
	if err != nil || !exists {
		return false, err
	}
	return columnIsInteger(ctx, tx, dialectName, table, column)
}

// columnIsInteger reports whether a table column holds an integer identity
// (the pre-UUID sentinel). SQLite types are read from pragma_table_info,
// PostgreSQL from information_schema.
func columnIsInteger(ctx context.Context, tx *sql.Tx, dialectName, table, column string) (bool, error) {
	var typ string
	if isPostgres(dialectName) {
		err := tx.QueryRowContext(ctx,
			`SELECT data_type FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1 AND column_name = $2`,
			table, column).Scan(&typ)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("read column type %s.%s: %w", table, column, err)
		}
	} else {
		err := tx.QueryRowContext(ctx,
			`SELECT type FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&typ)
		if err == sql.ErrNoRows {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("read column type %s.%s: %w", table, column, err)
		}
	}
	switch typ {
	case "integer", "bigint", "int", "smallint", "bigserial", "serial", "INTEGER", "BIGINT":
		return true, nil
	default:
		return false, nil
	}
}

// readIDMap loads every row's integer id and pairs it with a fresh UUID's raw
// 16 bytes.
func readIDMap(ctx context.Context, tx *sql.Tx, dialectName, table string) (map[int64][]byte, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT "id" FROM "%s"`, table))
	if err != nil {
		return nil, fmt.Errorf("read %s ids: %w", table, err)
	}
	defer rows.Close()
	m := make(map[int64][]byte)
	for rows.Next() {
		var oldID int64
		if err := rows.Scan(&oldID); err != nil {
			return nil, fmt.Errorf("scan %s id: %w", table, err)
		}
		u := ident.New()
		m[oldID] = u[:]
	}
	return m, rows.Err()
}

func createIDMap(ctx context.Context, tx *sql.Tx, dialectName string, d idDialect, name string, m map[int64][]byte) error {
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE "%s" ("old" %s NOT NULL, "new" %s NOT NULL)`, name, d.integer, d.uuid)); err != nil {
		return fmt.Errorf("create id map %s: %w", name, err)
	}
	stmt, err := tx.PrepareContext(ctx, fmt.Sprintf(
		`INSERT INTO "%s" ("old", "new") VALUES (%s, %s)`, name, placeholder(dialectName, 1), placeholder(dialectName, 2)))
	if err != nil {
		return fmt.Errorf("prepare id map %s: %w", name, err)
	}
	defer stmt.Close()
	for oldID, newID := range m {
		if _, err := stmt.ExecContext(ctx, oldID, newID); err != nil {
			return fmt.Errorf("insert id map %s: %w", name, err)
		}
	}
	return nil
}

// idColumn describes one column of a rebuilt table.
type idColumn struct {
	name     string
	typ      string
	nullable bool
	unique   bool
}

// idIndex describes one index to recreate after a rebuild.
type idIndex struct {
	name    string
	unique  bool
	columns []string
}

// rebuildIDTable recreates a table with a new column set, copying rows while
// remapping id columns through the generated maps. It is used for tables whose
// primary key changes (which SQLite cannot alter in place).
func rebuildIDTable(ctx context.Context, tx *sql.Tx, table string, cols []idColumn, pk []string, selectExpr map[string]string, from string, indexes []idIndex) error {
	tmp := table + "__mr_uuid"

	var defs []string
	var names []string
	var exprs []string
	for _, c := range cols {
		def := fmt.Sprintf("%q %s", c.name, c.typ)
		if c.unique {
			def += " UNIQUE"
		}
		if !c.nullable {
			def += " NOT NULL"
		}
		defs = append(defs, def)
		names = append(names, fmt.Sprintf("%q", c.name))
		exprs = append(exprs, selectExpr[c.name])
	}
	pkq := make([]string, len(pk))
	for i, c := range pk {
		pkq[i] = fmt.Sprintf("%q", c)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`CREATE TABLE "%s" (%s, PRIMARY KEY (%s))`, tmp, joinComma(defs), joinComma(pkq))); err != nil {
		return fmt.Errorf("create %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`INSERT INTO "%s" (%s) SELECT %s %s`, tmp, joinComma(names), joinComma(exprs), from)); err != nil {
		return fmt.Errorf("copy %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE "%s"`, table)); err != nil {
		return fmt.Errorf("drop %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" RENAME TO "%s"`, tmp, table)); err != nil {
		return fmt.Errorf("rename %s: %w", table, err)
	}
	for _, idx := range indexes {
		unq := ""
		if idx.unique {
			unq = "UNIQUE "
		}
		cols := make([]string, len(idx.columns))
		for i, c := range idx.columns {
			cols[i] = fmt.Sprintf("%q", c)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`CREATE %sINDEX "%s" ON "%s" (%s)`, unq, idx.name, table, joinComma(cols))); err != nil {
			return fmt.Errorf("create index %s: %w", idx.name, err)
		}
	}
	return nil
}

// convertUserIDColumn remaps a non-key user_id column in place (add, update,
// swap) for tables whose primary key is unchanged, recreating the indexes that
// reference the column with the same definitions.
func convertUserIDColumn(ctx context.Context, tx *sql.Tx, dialectName string, d idDialect, table string, indexes []idIndex) error {
	newCol := "user_id__mr_uuid"
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`ALTER TABLE "%s" ADD COLUMN "%s" %s`, table, newCol, d.uuid)); err != nil {
		return fmt.Errorf("add user_id column to %s: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`UPDATE "%s" SET "%s" = COALESCE((SELECT "new" FROM "__mr_user_id_map" WHERE "old" = "%s"."user_id"), %s)`,
		table, newCol, table, d.zeroUUIDExpr())); err != nil {
		return fmt.Errorf("remap %s user_id: %w", table, err)
	}
	for _, idx := range indexes {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP INDEX IF EXISTS "%s"`, idx.name)); err != nil {
			return fmt.Errorf("drop index %s: %w", idx.name, err)
		}
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE "%s" DROP COLUMN "user_id"`, table)); err != nil {
		return fmt.Errorf("drop %s user_id: %w", table, err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`ALTER TABLE "%s" RENAME COLUMN "%s" TO "user_id"`, table, newCol)); err != nil {
		return fmt.Errorf("rename %s user_id: %w", table, err)
	}
	for _, idx := range indexes {
		unq := ""
		if idx.unique {
			unq = "UNIQUE "
		}
		cols := make([]string, len(idx.columns))
		for i, c := range idx.columns {
			cols[i] = fmt.Sprintf("%q", c)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`CREATE %sINDEX "%s" ON "%s" (%s)`, unq, idx.name, table, joinComma(cols))); err != nil {
			return fmt.Errorf("create index %s: %w", idx.name, err)
		}
	}
	return nil
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// mapExpr returns a SELECT expression that looks the column's value up in the
// user id map, falling back to the zero UUID for unowned rows.
func mapExpr(d idDialect, column string) string {
	return fmt.Sprintf(`COALESCE("__mr_user_id_map"."new", %s)`, d.zeroUUIDExpr())
}

func rebuildUsers(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "id", typ: d.uuid},
		{name: "username", typ: d.text, unique: true},
		{name: "password_hash", typ: d.text},
		{name: "role", typ: d.text},
		{name: "status", typ: d.text},
		{name: "token_version", typ: d.integer},
		{name: "created_at", typ: d.timestamp},
		{name: "updated_at", typ: d.timestamp},
	}
	return rebuildIDTable(ctx, tx, "users", cols, []string{"id"}, map[string]string{
		"id":            `"__mr_user_id_map"."new"`,
		"username":      `"users"."username"`,
		"password_hash": `"users"."password_hash"`,
		"role":          `"users"."role"`,
		"status":        `"users"."status"`,
		"token_version": `"users"."token_version"`,
		"created_at":    `"users"."created_at"`,
		"updated_at":    `"users"."updated_at"`,
	}, `FROM "users" INNER JOIN "__mr_user_id_map" ON "__mr_user_id_map"."old" = "users"."id"`, []idIndex{
		{name: "user_role", columns: []string{"role"}},
		{name: "user_status", columns: []string{"status"}},
	})
}

func rebuildBookshelf(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "id", typ: d.uuid},
		{name: "user_id", typ: d.uuid},
		{name: "gallery_id", typ: d.integer},
		{name: "token", typ: d.text},
		{name: "created_at", typ: d.timestamp},
		{name: "updated_at", typ: d.timestamp},
	}
	return rebuildIDTable(ctx, tx, "bookshelf", cols, []string{"id"}, map[string]string{
		"id":         `"__mr_bookshelf_id_map"."new"`,
		"user_id":    mapExpr(d, "user_id"),
		"gallery_id": `"bookshelf"."gallery_id"`,
		"token":      `"bookshelf"."token"`,
		"created_at": `"bookshelf"."created_at"`,
		"updated_at": `"bookshelf"."updated_at"`,
	}, `FROM "bookshelf" LEFT JOIN "__mr_bookshelf_id_map" ON "__mr_bookshelf_id_map"."old" = "bookshelf"."id" LEFT JOIN "__mr_user_id_map" ON "__mr_user_id_map"."old" = "bookshelf"."user_id"`, []idIndex{
		{name: "bookshelf_user_id_gallery_id_token", unique: true, columns: []string{"user_id", "gallery_id", "token"}},
	})
}

func rebuildReadingProgress(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "id", typ: d.uuid},
		{name: "user_id", typ: d.uuid},
		{name: "gallery_id", typ: d.integer},
		{name: "token", typ: d.text},
		{name: "current_page", typ: d.integer},
		{name: "progress", typ: d.real},
		{name: "completed", typ: d.boolean},
		{name: "created_at", typ: d.timestamp},
		{name: "updated_at", typ: d.timestamp},
	}
	return rebuildIDTable(ctx, tx, "reading_progress", cols, []string{"id"}, map[string]string{
		"id":           `"__mr_reading_progress_id_map"."new"`,
		"user_id":      mapExpr(d, "user_id"),
		"gallery_id":   `"reading_progress"."gallery_id"`,
		"token":        `"reading_progress"."token"`,
		"current_page": `"reading_progress"."current_page"`,
		"progress":     `"reading_progress"."progress"`,
		"completed":    `"reading_progress"."completed"`,
		"created_at":   `"reading_progress"."created_at"`,
		"updated_at":   `"reading_progress"."updated_at"`,
	}, `FROM "reading_progress" LEFT JOIN "__mr_reading_progress_id_map" ON "__mr_reading_progress_id_map"."old" = "reading_progress"."id" LEFT JOIN "__mr_user_id_map" ON "__mr_user_id_map"."old" = "reading_progress"."user_id"`, []idIndex{
		{name: "readingprogress_user_id_gallery_id_token", unique: true, columns: []string{"user_id", "gallery_id", "token"}},
	})
}

func rebuildUserSetting(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "user_id", typ: d.uuid},
		{name: "cookie_ciphertext", typ: d.text},
		{name: "cookie_status", typ: d.text},
		{name: "cookie_verified_at", typ: d.timestamp, nullable: true},
		{name: "site", typ: d.text},
		{name: "preferences", typ: d.json, nullable: true},
		{name: "created_at", typ: d.timestamp},
		{name: "updated_at", typ: d.timestamp},
	}
	return rebuildIDTable(ctx, tx, "user_setting", cols, []string{"user_id"}, map[string]string{
		"user_id":            mapExpr(d, "user_id"),
		"cookie_ciphertext":  `"user_setting"."cookie_ciphertext"`,
		"cookie_status":      `"user_setting"."cookie_status"`,
		"cookie_verified_at": `"user_setting"."cookie_verified_at"`,
		"site":               `"user_setting"."site"`,
		"preferences":        `"user_setting"."preferences"`,
		"created_at":         `"user_setting"."created_at"`,
		"updated_at":         `"user_setting"."updated_at"`,
	}, `FROM "user_setting" LEFT JOIN "__mr_user_id_map" ON "__mr_user_id_map"."old" = "user_setting"."user_id"`, nil)
}

func rebuildTagPreference(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "user_id", typ: d.uuid},
		{name: "action", typ: d.text},
		{name: "weight", typ: d.integer},
		{name: "created_at", typ: d.timestamp},
		{name: "updated_at", typ: d.timestamp},
		{name: "tag", typ: d.text},
	}
	return rebuildIDTable(ctx, tx, "tag_preference", cols, []string{"user_id", "tag"}, map[string]string{
		"user_id":    mapExpr(d, "user_id"),
		"action":     `"tag_preference"."action"`,
		"weight":     `"tag_preference"."weight"`,
		"created_at": `"tag_preference"."created_at"`,
		"updated_at": `"tag_preference"."updated_at"`,
		"tag":        `"tag_preference"."tag"`,
	}, `FROM "tag_preference" LEFT JOIN "__mr_user_id_map" ON "__mr_user_id_map"."old" = "tag_preference"."user_id"`, nil)
}

func rebuildTagSyncBaseline(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "user_id", typ: d.uuid},
		{name: "action", typ: d.text},
		{name: "weight", typ: d.integer},
		{name: "created_at", typ: d.timestamp},
		{name: "updated_at", typ: d.timestamp},
		{name: "tag", typ: d.text},
	}
	return rebuildIDTable(ctx, tx, "tag_sync_baseline", cols, []string{"user_id", "tag"}, map[string]string{
		"user_id":    mapExpr(d, "user_id"),
		"action":     `"tag_sync_baseline"."action"`,
		"weight":     `"tag_sync_baseline"."weight"`,
		"created_at": `"tag_sync_baseline"."created_at"`,
		"updated_at": `"tag_sync_baseline"."updated_at"`,
		"tag":        `"tag_sync_baseline"."tag"`,
	}, `FROM "tag_sync_baseline" LEFT JOIN "__mr_user_id_map" ON "__mr_user_id_map"."old" = "tag_sync_baseline"."user_id"`, nil)
}

func rebuildNode(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "id", typ: d.uuid},
		{name: "name", typ: d.text},
		{name: "status", typ: d.text},
		{name: "last_seen_at", typ: d.timestamp, nullable: true},
		{name: "created_at", typ: d.timestamp},
		{name: "updated_at", typ: d.timestamp},
	}
	return rebuildIDTable(ctx, tx, "node", cols, []string{"id"}, map[string]string{
		"id":           `"__mr_node_id_map"."new"`,
		"name":         `"node"."name"`,
		"status":       `"node"."status"`,
		"last_seen_at": `"node"."last_seen_at"`,
		"created_at":   `"node"."created_at"`,
		"updated_at":   `"node"."updated_at"`,
	}, `FROM "node" INNER JOIN "__mr_node_id_map" ON "__mr_node_id_map"."old" = "node"."id"`, []idIndex{
		{name: "node_name", unique: true, columns: []string{"name"}},
		{name: "node_status", columns: []string{"status"}},
	})
}

func rebuildNodeAPIKey(ctx context.Context, tx *sql.Tx, d idDialect) error {
	cols := []idColumn{
		{name: "id", typ: d.uuid},
		{name: "node_id", typ: d.uuid},
		{name: "key_hash", typ: d.text, unique: true},
		{name: "key_prefix", typ: d.text},
		{name: "scopes", typ: d.json, nullable: true},
		{name: "status", typ: d.text},
		{name: "expires_at", typ: d.timestamp, nullable: true},
		{name: "last_used_at", typ: d.timestamp, nullable: true},
		{name: "created_at", typ: d.timestamp},
		{name: "revoked_at", typ: d.timestamp, nullable: true},
	}
	return rebuildIDTable(ctx, tx, "node_api_key", cols, []string{"id"}, map[string]string{
		"id":           `"__mr_key_id_map"."new"`,
		"node_id":      `COALESCE("__mr_node_id_map"."new", ` + d.zeroUUIDExpr() + `)`,
		"key_hash":     `"node_api_key"."key_hash"`,
		"key_prefix":   `"node_api_key"."key_prefix"`,
		"scopes":       `"node_api_key"."scopes"`,
		"status":       `"node_api_key"."status"`,
		"expires_at":   `"node_api_key"."expires_at"`,
		"last_used_at": `"node_api_key"."last_used_at"`,
		"created_at":   `"node_api_key"."created_at"`,
		"revoked_at":   `"node_api_key"."revoked_at"`,
	}, `FROM "node_api_key" LEFT JOIN "__mr_key_id_map" ON "__mr_key_id_map"."old" = "node_api_key"."id" LEFT JOIN "__mr_node_id_map" ON "__mr_node_id_map"."old" = "node_api_key"."node_id"`, []idIndex{
		{name: "nodeapikey_node_id_status", columns: []string{"node_id", "status"}},
		{name: "nodeapikey_key_prefix", columns: []string{"key_prefix"}},
	})
}
