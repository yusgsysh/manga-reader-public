package config

// Default tenancy values. Shared Database + Shared Schema isolation gives
// existing single-tenant installations a stable default tenant; the data
// migration assigns all pre-multi-tenant rows to it.
const (
	DefaultTenantID   int64 = 1
	DefaultTenantSlug       = "default"
	DefaultTenantName       = "Default"
)

// Roles and statuses shared by auth, management handlers and migrations.
const (
	RolePlatformAdmin = "platform_admin"
	RoleTenantAdmin   = "tenant_admin"
	RoleUser          = "user"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)
