package handler

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/tenant"
	"manga-reader/internal/ent/user"
	"manga-reader/internal/web"
)

var tenantSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type tenantJSON struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func tenantResponse(t *ent.Tenant) tenantJSON {
	return tenantJSON{
		ID:        int64(t.ID),
		Name:      t.Name,
		Slug:      t.Slug,
		Status:    t.Status,
		CreatedAt: t.CreatedAt.UTC().Format(timeLayout),
		UpdatedAt: t.UpdatedAt.UTC().Format(timeLayout),
	}
}

const timeLayout = "2006-01-02T15:04:05Z07:00"

func validTenantStatus(value string) bool {
	return value == config.StatusActive || value == config.StatusDisabled
}

func validUserStatus(value string) bool {
	return value == config.StatusActive || value == config.StatusDisabled
}

// ==================== Platform administration ====================

func (s *Server) handleAdminTenantCreate(c *gin.Context) {
	if !s.requirePlatformAdmin(c) {
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		Name          string `json:"name"`
		Slug          string `json:"slug"`
		AdminUsername string `json:"admin_username"`
		AdminPassword string `json:"admin_password"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Slug = strings.ToLower(strings.TrimSpace(body.Slug))
	body.AdminUsername = strings.TrimSpace(body.AdminUsername)
	if body.Name == "" || !tenantSlugPattern.MatchString(body.Slug) {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "name and a valid slug are required"})
		return
	}
	if (body.AdminUsername == "") != (body.AdminPassword == "") {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "admin_username and admin_password must be provided together"})
		return
	}
	if body.AdminPassword != "" && len(body.AdminPassword) < 8 {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "admin_password must be at least 8 characters"})
		return
	}

	var passwordHash string
	if body.AdminPassword != "" {
		hash, err := auth.HashPassword(body.AdminPassword)
		if err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "password hashing failed"})
			return
		}
		passwordHash = hash
	}

	ctx := c.Request.Context()
	tx, err := s.DB.Client.Tx(ctx)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create tenant failed"})
		return
	}
	defer func() { _ = tx.Rollback() }()

	created, err := tx.Tenant.Create().
		SetName(body.Name).
		SetSlug(body.Slug).
		SetStatus(config.StatusActive).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "tenant slug already exists"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create tenant failed"})
		return
	}
	if body.AdminUsername != "" {
		if _, err := tx.User.Create().
			SetTenantID(int64(created.ID)).
			SetUsername(body.AdminUsername).
			SetPasswordHash(passwordHash).
			SetRole(config.RoleTenantAdmin).
			SetStatus(config.StatusActive).
			Save(ctx); err != nil {
			if ent.IsConstraintError(err) {
				web.JSON(c, http.StatusConflict, gin.H{"error": "admin username already exists in this tenant"})
				return
			}
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create tenant admin failed"})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create tenant failed"})
		return
	}
	web.JSON(c, http.StatusCreated, tenantResponse(created))
}

func (s *Server) handleAdminTenantList(c *gin.Context) {
	if !s.requirePlatformAdmin(c) {
		return
	}
	rows, err := s.DB.Client.Tenant.Query().
		Order(ent.Asc(tenant.FieldID)).
		All(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "list tenants failed"})
		return
	}
	out := make([]tenantJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, tenantResponse(row))
	}
	web.JSON(c, http.StatusOK, gin.H{"tenants": out})
}

func (s *Server) handleAdminTenantGet(c *gin.Context) {
	if !s.requirePlatformAdmin(c) {
		return
	}
	id, ok := adminID(c)
	if !ok {
		return
	}
	t, err := s.DB.Client.Tenant.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read tenant failed"})
		return
	}
	web.JSON(c, http.StatusOK, tenantResponse(t))
}

func (s *Server) handleAdminTenantUpdate(c *gin.Context) {
	if !s.requirePlatformAdmin(c) {
		return
	}
	id, ok := adminID(c)
	if !ok {
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		Name *string `json:"name"`
		Slug *string `json:"slug"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	update := s.DB.Client.Tenant.UpdateOneID(id)
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "name must not be empty"})
			return
		}
		update.SetName(name)
	}
	if body.Slug != nil {
		slug := strings.ToLower(strings.TrimSpace(*body.Slug))
		if !tenantSlugPattern.MatchString(slug) {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid slug"})
			return
		}
		update.SetSlug(slug)
	}
	t, err := update.Save(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "tenant slug already exists"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "update tenant failed"})
		return
	}
	web.JSON(c, http.StatusOK, tenantResponse(t))
}

func (s *Server) handleAdminTenantStatus(c *gin.Context) {
	if !s.requirePlatformAdmin(c) {
		return
	}
	id, ok := adminID(c)
	if !ok {
		return
	}
	limitRequestBody(c, 8<<10)
	var body struct {
		Status string `json:"status"`
	}
	if err := web.BindJSON(c, &body); err != nil || !validTenantStatus(body.Status) {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "status must be active or disabled"})
		return
	}
	t, err := s.DB.Client.Tenant.UpdateOneID(id).SetStatus(body.Status).Save(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "tenant not found"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "update tenant status failed"})
		return
	}
	web.JSON(c, http.StatusOK, tenantResponse(t))
}

// ==================== Tenant administration ====================

type tenantUserJSON struct {
	ID        int    `json:"id"`
	TenantID  int64  `json:"tenant_id"`
	Username  string `json:"username"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

func tenantUserResponse(u *ent.User) tenantUserJSON {
	return tenantUserJSON{
		ID:        u.ID,
		TenantID:  u.TenantID,
		Username:  u.Username,
		Role:      u.Role,
		Status:    u.Status,
		CreatedAt: u.CreatedAt.UTC().Format(timeLayout),
		UpdatedAt: u.UpdatedAt.UTC().Format(timeLayout),
	}
}

func (s *Server) handleTenantUserList(c *gin.Context) {
	if !s.requireTenantAdmin(c) {
		return
	}
	tid, ok := s.requireTenant(c)
	if !ok {
		return
	}
	rows, err := s.DB.Client.User.Query().
		Where(user.TenantIDEQ(tid)).
		Order(ent.Asc(user.FieldID)).
		All(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "list users failed"})
		return
	}
	out := make([]tenantUserJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, tenantUserResponse(row))
	}
	web.JSON(c, http.StatusOK, gin.H{"users": out})
}

func (s *Server) handleTenantUserCreate(c *gin.Context) {
	if !s.requireTenantAdmin(c) {
		return
	}
	tid, ok := s.requireTenant(c)
	if !ok {
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
		Status   string `json:"status"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || len(body.Password) < 8 {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "username and a password of at least 8 characters are required"})
		return
	}
	if body.Role == "" {
		body.Role = config.RoleUser
	}
	if body.Role != config.RoleUser && body.Role != config.RoleTenantAdmin {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "role must be user or tenant_admin"})
		return
	}
	if body.Status == "" {
		body.Status = config.StatusActive
	}
	if !validUserStatus(body.Status) {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "status must be active or disabled"})
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "password hashing failed"})
		return
	}
	created, err := s.DB.Client.User.Create().
		SetTenantID(tid).
		SetUsername(body.Username).
		SetPasswordHash(hash).
		SetRole(body.Role).
		SetStatus(body.Status).
		Save(c.Request.Context())
	if err != nil {
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "username already exists in this tenant"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create user failed"})
		return
	}
	web.JSON(c, http.StatusCreated, tenantUserResponse(created))
}

func (s *Server) handleTenantUserUpdate(c *gin.Context) {
	if !s.requireTenantAdmin(c) {
		return
	}
	tid, ok := s.requireTenant(c)
	if !ok {
		return
	}
	uid, ok := adminID(c)
	if !ok {
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		Username *string `json:"username"`
		Password *string `json:"password"`
		Role     *string `json:"role"`
		Status   *string `json:"status"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	existing, err := s.DB.Client.User.Query().
		Where(user.ID(uid), user.TenantIDEQ(tid)).
		Only(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if existing.Role == config.RolePlatformAdmin {
		web.JSON(c, http.StatusForbidden, gin.H{"error": "platform administrators are managed by the platform"})
		return
	}

	update := s.DB.Client.User.UpdateOneID(existing.ID)
	revoke := false
	if body.Username != nil {
		name := strings.TrimSpace(*body.Username)
		if name == "" {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "username must not be empty"})
			return
		}
		update.SetUsername(name)
	}
	if body.Password != nil {
		if len(*body.Password) < 8 {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
			return
		}
		hash, err := auth.HashPassword(*body.Password)
		if err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "password hashing failed"})
			return
		}
		update.SetPasswordHash(hash)
		revoke = true
	}
	if body.Role != nil {
		if *body.Role != config.RoleUser && *body.Role != config.RoleTenantAdmin {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "role must be user or tenant_admin"})
			return
		}
		update.SetRole(*body.Role)
		revoke = true
	}
	if body.Status != nil {
		if !validUserStatus(*body.Status) {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "status must be active or disabled"})
			return
		}
		update.SetStatus(*body.Status)
		if *body.Status == config.StatusDisabled {
			revoke = true
		}
	}
	if revoke {
		update.AddTokenVersion(1)
	}
	updated, err := update.Save(c.Request.Context())
	if err != nil {
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "username already exists in this tenant"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "update user failed"})
		return
	}
	web.JSON(c, http.StatusOK, tenantUserResponse(updated))
}

func (s *Server) handleTenantUserStatus(c *gin.Context) {
	if !s.requireTenantAdmin(c) {
		return
	}
	tid, ok := s.requireTenant(c)
	if !ok {
		return
	}
	uid, ok := adminID(c)
	if !ok {
		return
	}
	limitRequestBody(c, 8<<10)
	var body struct {
		Status string `json:"status"`
	}
	if err := web.BindJSON(c, &body); err != nil || !validUserStatus(body.Status) {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "status must be active or disabled"})
		return
	}
	existing, err := s.DB.Client.User.Query().
		Where(user.ID(uid), user.TenantIDEQ(tid)).
		Only(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if existing.Role == config.RolePlatformAdmin {
		web.JSON(c, http.StatusForbidden, gin.H{"error": "platform administrators are managed by the platform"})
		return
	}
	update := s.DB.Client.User.UpdateOneID(existing.ID).SetStatus(body.Status)
	if body.Status == config.StatusDisabled {
		update.AddTokenVersion(1)
	}
	updated, err := update.Save(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "update user status failed"})
		return
	}
	web.JSON(c, http.StatusOK, tenantUserResponse(updated))
}

func adminID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid id %q", c.Param("id"))})
		return 0, false
	}
	return id, true
}
