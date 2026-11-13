package database

import (
	"context"
	"fmt"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/tenant"
)

// migrateTenancy brings pre-multi-tenancy databases onto the shared-schema
// tenant model. It runs after Ent's structural migration (which adds the new
// columns with DEFAULT 1 and rebuilds composite primary keys) and before the
// server serves requests.
//
// The whole data step is one transaction: a failure rolls back tenant creation
// and row reassignment together, so a retry starts from a consistent state.
func migrateTenancy(ctx context.Context, client *ent.Client) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// A database with no tenant rows is either brand new or from the
	// single-tenant version. In the latter case every existing user becomes
	// the platform administrator of the default tenant.
	tenantCount, err := tx.Tenant.Query().Count(ctx)
	if err != nil {
		return fmt.Errorf("count tenants: %w", err)
	}
	legacy := tenantCount == 0

	defaultTenant, err := tx.Tenant.Query().
		Where(tenant.SlugEQ(config.DefaultTenantSlug)).
		Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return fmt.Errorf("find default tenant: %w", err)
		}
		build := tx.Tenant.Create().
			SetName(config.DefaultTenantName).
			SetSlug(config.DefaultTenantSlug).
			SetStatus(config.StatusActive)
		defaultTenant, err = build.Save(ctx)
		if err != nil {
			return fmt.Errorf("create default tenant: %w", err)
		}
	}

	if legacy {
		// Existing pre-JWT-in-multi-tenant users were the single owner of the
		// installation; promote them so an upgrade does not lock the operator
		// out of tenant administration.
		if _, err := tx.User.Update().
			SetTenantID(int64(defaultTenant.ID)).
			SetRole(config.RolePlatformAdmin).
			SetStatus(config.StatusActive).
			Save(ctx); err != nil {
			return fmt.Errorf("assign legacy users: %w", err)
		}

		// New columns default to 1, but be explicit: this also repairs a
		// partially-migrated database where the default was not applied.
		// GalleryCache is intentionally global and is not touched.
		if _, err := tx.Bookshelf.Update().SetTenantID(int64(defaultTenant.ID)).Save(ctx); err != nil {
			return fmt.Errorf("assign bookshelf rows: %w", err)
		}
		if _, err := tx.ReadingProgress.Update().SetTenantID(int64(defaultTenant.ID)).Save(ctx); err != nil {
			return fmt.Errorf("assign reading progress rows: %w", err)
		}
		if _, err := tx.PrefillJob.Update().SetTenantID(int64(defaultTenant.ID)).Save(ctx); err != nil {
			return fmt.Errorf("assign prefill jobs: %w", err)
		}
		if _, err := tx.TagPreference.Update().SetTenantID(int64(defaultTenant.ID)).Save(ctx); err != nil {
			return fmt.Errorf("assign tag preferences: %w", err)
		}
		if _, err := tx.TagSyncBaseline.Update().SetTenantID(int64(defaultTenant.ID)).Save(ctx); err != nil {
			return fmt.Errorf("assign tag baselines: %w", err)
		}
		if _, err := tx.SyncChange.Update().SetTenantID(int64(defaultTenant.ID)).Save(ctx); err != nil {
			return fmt.Errorf("assign sync changes: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
