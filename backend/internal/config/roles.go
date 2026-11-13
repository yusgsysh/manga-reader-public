package config

// Roles and account statuses shared by auth, management handlers and
// migrations. Accounts are instance-wide: admin manages the installation,
// user only owns their private data.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)
