package settings

import (
	"context"
	"fmt"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/setting"
)

// readOverrides returns the persisted rows restricted to managed keys, so a
// row left behind by an older release can never shadow the environment.
func readOverrides(ctx context.Context, client *ent.Client) (map[string]string, error) {
	rows, err := client.Setting.Query().Where(setting.KeyIn(config.ManagedKeys...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read settings: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}

// writeOverrides makes the table hold exactly overrides for managed keys: keys
// that reverted to their environment value are deleted, changed keys are
// inserted or updated. Everything happens in one transaction so a crash cannot
// leave a half-saved configuration behind.
func writeOverrides(ctx context.Context, client *ent.Client, overrides map[string]string) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin settings transaction: %w", err)
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	rows, err := tx.Setting.Query().Where(setting.KeyIn(config.ManagedKeys...)).All(ctx)
	if err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	existing := make(map[string]*ent.Setting, len(rows))
	for _, row := range rows {
		existing[row.Key] = row
	}

	for _, key := range config.ManagedKeys {
		value, wanted := overrides[key]
		row, present := existing[key]
		switch {
		case wanted && present:
			if row.Value != value {
				if err := row.Update().SetValue(value).Exec(ctx); err != nil {
					return fmt.Errorf("update setting %s: %w", key, err)
				}
			}
		case wanted && !present:
			if err := tx.Setting.Create().SetKey(key).SetValue(value).Exec(ctx); err != nil {
				return fmt.Errorf("create setting %s: %w", key, err)
			}
		case !wanted && present:
			if err := tx.Setting.DeleteOne(row).Exec(ctx); err != nil {
				return fmt.Errorf("delete setting %s: %w", key, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings: %w", err)
	}
	tx = nil
	return nil
}
