package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ident"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/prefilljob"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/ent/syncchange"
	"manga-reader/internal/ent/tagpreference"
	"manga-reader/internal/ent/tagsyncbaseline"
	"manga-reader/internal/ent/user"
	"manga-reader/internal/ent/usersetting"
	"manga-reader/internal/web"
)

const timeLayout = "2006-01-02T15:04:05Z07:00"

type userJSON struct {
	ID        ident.UUID `json:"id"`
	UserID    ident.UUID `json:"user_id"`
	Username  string     `json:"username"`
	Role      string     `json:"role"`
	Status    string     `json:"status"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

func userResponse(u *ent.User) userJSON {
	return userJSON{
		ID:        u.ID,
		UserID:    u.ID,
		Username:  u.Username,
		Role:      u.Role,
		Status:    u.Status,
		CreatedAt: u.CreatedAt.UTC().Format(timeLayout),
		UpdatedAt: u.UpdatedAt.UTC().Format(timeLayout),
	}
}

func validRole(value string) bool {
	return value == config.RoleAdmin || value == config.RoleUser
}

func validUserStatus(value string) bool {
	return value == config.StatusActive || value == config.StatusDisabled
}

func (s *Server) handleAdminUserList(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	rows, err := s.DB.Client.User.Query().
		Order(ent.Asc(user.FieldID)).
		All(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "list users failed"})
		return
	}
	out := make([]userJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, userResponse(row))
	}
	web.JSON(c, http.StatusOK, gin.H{"users": out})
}

func (s *Server) handleAdminUserGet(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, ok := adminID(c)
	if !ok {
		return
	}
	u, err := s.DB.Client.User.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "user not found"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read user failed"})
		return
	}
	web.JSON(c, http.StatusOK, userResponse(u))
}

func (s *Server) handleAdminUserCreate(c *gin.Context) {
	if !s.requireAdmin(c) {
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
	if !validRole(body.Role) {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "role must be admin or user"})
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
		SetUsername(body.Username).
		SetPasswordHash(hash).
		SetRole(body.Role).
		SetStatus(body.Status).
		Save(c.Request.Context())
	if err != nil {
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create user failed"})
		return
	}
	audit(c, "user.create", "target_user_id", created.ID, "role", created.Role)
	web.JSON(c, http.StatusCreated, userResponse(created))
}

func (s *Server) handleAdminUserUpdate(c *gin.Context) {
	if !s.requireAdmin(c) {
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
	existing, err := s.DB.Client.User.Get(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "user not found"})
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
		if !validRole(*body.Role) {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "role must be admin or user"})
			return
		}
		if existing.Role == config.RoleAdmin && *body.Role != config.RoleAdmin {
			if !s.otherAdminExists(c, existing.ID) {
				web.JSON(c, http.StatusConflict, gin.H{"error": "the last administrator cannot be demoted"})
				return
			}
		}
		update.SetRole(*body.Role)
		revoke = true
	}
	if body.Status != nil {
		if !validUserStatus(*body.Status) {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "status must be active or disabled"})
			return
		}
		if existing.Role == config.RoleAdmin && *body.Status == config.StatusDisabled && !s.otherAdminExists(c, existing.ID) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "the last administrator cannot be disabled"})
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
			web.JSON(c, http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "update user failed"})
		return
	}
	audit(c, "user.update", "target_user_id", updated.ID, "role", updated.Role, "status", updated.Status)
	web.JSON(c, http.StatusOK, userResponse(updated))
}

func (s *Server) handleAdminUserStatus(c *gin.Context) {
	if !s.requireAdmin(c) {
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
	existing, err := s.DB.Client.User.Get(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if existing.Role == config.RoleAdmin && body.Status == config.StatusDisabled && !s.otherAdminExists(c, existing.ID) {
		web.JSON(c, http.StatusConflict, gin.H{"error": "the last administrator cannot be disabled"})
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
	audit(c, "user.status", "target_user_id", updated.ID, "role", updated.Role, "status", updated.Status)
	web.JSON(c, http.StatusOK, userResponse(updated))
}

func (s *Server) handleAdminUserDelete(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	uid, ok := adminID(c)
	if !ok {
		return
	}
	principal, _ := auth.PrincipalFrom(c)
	if principal.ID == uid {
		web.JSON(c, http.StatusConflict, gin.H{"error": "you cannot delete your own account"})
		return
	}
	existing, err := s.DB.Client.User.Get(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	if existing.Role == config.RoleAdmin && !s.otherAdminExists(c, existing.ID) {
		web.JSON(c, http.StatusConflict, gin.H{"error": "the last administrator cannot be deleted"})
		return
	}

	ctx := c.Request.Context()
	tx, err := s.DB.Client.Tx(ctx)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user failed"})
		return
	}
	defer func() { _ = tx.Rollback() }()

	userID := uid
	if _, err := tx.Bookshelf.Delete().Where(bookshelf.UserIDEQ(userID)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user data failed"})
		return
	}
	if _, err := tx.ReadingProgress.Delete().Where(readingprogress.UserIDEQ(userID)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user data failed"})
		return
	}
	if _, err := tx.PrefillJob.Delete().Where(prefilljob.UserIDEQ(userID)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user data failed"})
		return
	}
	if _, err := tx.TagPreference.Delete().Where(tagpreference.UserIDEQ(userID)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user data failed"})
		return
	}
	if _, err := tx.TagSyncBaseline.Delete().Where(tagsyncbaseline.UserIDEQ(userID)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user data failed"})
		return
	}
	if _, err := tx.SyncChange.Delete().Where(syncchange.UserIDEQ(userID)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user data failed"})
		return
	}
	if _, err := tx.UserSetting.Delete().Where(usersetting.IDEQ(uid)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user settings failed"})
		return
	}
	if err := tx.User.DeleteOneID(uid).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete user failed"})
		return
	}
	audit(c, "user.delete", "target_user_id", uid)
	s.cleanupGalleryCache(ctx)
	web.JSON(c, http.StatusOK, gin.H{"deleted": true})
}

func (s *Server) otherAdminExists(c *gin.Context, exceptID ident.UUID) bool {
	count, err := s.DB.Client.User.Query().
		Where(
			user.RoleEQ(config.RoleAdmin),
			user.StatusEQ(config.StatusActive),
			user.IDNEQ(exceptID),
		).
		Count(c.Request.Context())
	return err == nil && count > 0
}

func adminID(c *gin.Context) (ident.UUID, bool) {
	id, err := ident.Parse(c.Param("id"))
	if err != nil || id == ident.Nil() {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid id"})
		return ident.Nil(), false
	}
	return id, true
}
