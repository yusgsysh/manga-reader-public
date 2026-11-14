package handler

import (
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ident"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/node"
	"manga-reader/internal/ent/nodeapikey"
	nodeauth "manga-reader/internal/node"
	"manga-reader/internal/requestid"
	"manga-reader/internal/web"
)

type nodeJSON struct {
	ID         ident.UUID `json:"id"`
	Name       string     `json:"name"`
	Status     string     `json:"status"`
	LastSeenAt *string    `json:"last_seen_at"`
	CreatedAt  string     `json:"created_at"`
	UpdatedAt  string     `json:"updated_at"`
}

type nodeKeyJSON struct {
	ID         ident.UUID `json:"id"`
	NodeID     ident.UUID `json:"node_id"`
	KeyPrefix  string     `json:"key_prefix"`
	Scopes     []string   `json:"scopes"`
	Status     string     `json:"status"`
	ExpiresAt  *string    `json:"expires_at"`
	LastUsedAt *string    `json:"last_used_at"`
	CreatedAt  string     `json:"created_at"`
	RevokedAt  *string    `json:"revoked_at"`
}

func formatTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(timeLayout)
	return &s
}

func nodeResponse(n *ent.Node) nodeJSON {
	return nodeJSON{
		ID: n.ID, Name: n.Name, Status: n.Status,
		LastSeenAt: formatTimePtr(n.LastSeenAt),
		CreatedAt:  n.CreatedAt.UTC().Format(timeLayout),
		UpdatedAt:  n.UpdatedAt.UTC().Format(timeLayout),
	}
}

func nodeKeyResponse(k *ent.NodeAPIKey) nodeKeyJSON {
	return nodeKeyJSON{
		ID: k.ID, NodeID: k.NodeID, KeyPrefix: k.KeyPrefix, Scopes: k.Scopes,
		Status: k.Status, ExpiresAt: formatTimePtr(k.ExpiresAt),
		LastUsedAt: formatTimePtr(k.LastUsedAt),
		CreatedAt:  k.CreatedAt.UTC().Format(timeLayout),
		RevokedAt:  formatTimePtr(k.RevokedAt),
	}
}

func validNodeStatus(v string) bool {
	return v == config.StatusActive || v == config.StatusDisabled
}

func validateScopes(input []string) ([]string, bool) {
	if len(input) == 0 {
		return append([]string(nil), nodeauth.DefaultScopes...), true
	}
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, scope := range input {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			continue
		}
		allowed := false
		for _, a := range nodeauth.AllScopes {
			if scope == a {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, false
		}
		if _, ok := seen[scope]; ok {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	if len(out) == 0 {
		return nil, false
	}
	sort.Strings(out)
	return out, true
}

func audit(c *gin.Context, action string, attrs ...any) {
	base := []any{
		"request_id", requestid.From(c),
		"action", action,
		"result", "ok",
	}
	if principal, ok := auth.PrincipalFrom(c); ok {
		base = append(base, "actor_user_id", principal.ID)
	}
	slog.Info("security audit", append(base, attrs...)...)
}

func (s *Server) handleAdminNodeList(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	rows, err := s.DB.Client.Node.Query().Order(ent.Asc(node.FieldID)).All(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "list nodes failed"})
		return
	}
	out := make([]nodeJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, nodeResponse(row))
	}
	web.JSON(c, http.StatusOK, gin.H{"nodes": out})
}

func (s *Server) handleAdminNodeCreate(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if body.Name == "" {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if body.Status == "" {
		body.Status = config.StatusActive
	}
	if !validNodeStatus(body.Status) {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "status must be active or disabled"})
		return
	}
	created, err := s.DB.Client.Node.Create().
		SetName(body.Name).SetStatus(body.Status).
		Save(c.Request.Context())
	if err != nil {
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "node name already exists"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create node failed"})
		return
	}
	audit(c, "node.create", "node_id", created.ID)
	web.JSON(c, http.StatusCreated, nodeResponse(created))
}

func (s *Server) handleAdminNodeGet(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, ok := adminID(c)
	if !ok {
		return
	}
	n, err := s.DB.Client.Node.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "node not found"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read node failed"})
		return
	}
	web.JSON(c, http.StatusOK, nodeResponse(n))
}

func (s *Server) handleAdminNodeUpdate(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, ok := adminID(c)
	if !ok {
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		Name   *string `json:"name"`
		Status *string `json:"status"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	update := s.DB.Client.Node.UpdateOneID(id)
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "name must not be empty"})
			return
		}
		update.SetName(name)
	}
	if body.Status != nil {
		if !validNodeStatus(*body.Status) {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "status must be active or disabled"})
			return
		}
		update.SetStatus(*body.Status)
	}
	n, err := update.Save(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "node not found"})
			return
		}
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "node name already exists"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "update node failed"})
		return
	}
	audit(c, "node.update", "node_id", n.ID)
	web.JSON(c, http.StatusOK, nodeResponse(n))
}

func (s *Server) handleAdminNodeDelete(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	id, ok := adminID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	tx, err := s.DB.Client.Tx(ctx)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete node failed"})
		return
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.NodeAPIKey.Delete().Where(nodeapikey.NodeIDEQ(id)).Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete node keys failed"})
		return
	}
	if err := tx.Node.DeleteOneID(id).Exec(ctx); err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "node not found"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete node failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete node failed"})
		return
	}
	audit(c, "node.delete", "node_id", id)
	web.JSON(c, http.StatusOK, gin.H{"deleted": true})
}

func (s *Server) handleAdminNodeKeyList(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	nodeID, ok := nodeIDParam(c)
	if !ok {
		return
	}
	rows, err := s.DB.Client.NodeAPIKey.Query().
		Where(nodeapikey.NodeIDEQ(nodeID)).
		Order(ent.Desc(nodeapikey.FieldCreatedAt)).
		All(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "list node keys failed"})
		return
	}
	out := make([]nodeKeyJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, nodeKeyResponse(row))
	}
	web.JSON(c, http.StatusOK, gin.H{"keys": out})
}

func (s *Server) handleAdminNodeKeyCreate(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	nodeID, ok := nodeIDParam(c)
	if !ok {
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		Scopes        []string `json:"scopes"`
		ExpiresAt     *string  `json:"expires_at"`
		ExpiresInDays *int     `json:"expires_in_days"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	scopes, ok := validateScopes(body.Scopes)
	if !ok {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid scopes"})
		return
	}
	expires, ok := parseKeyExpiry(body.ExpiresAt, body.ExpiresInDays)
	if !ok {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid expiry"})
		return
	}
	if _, err := s.DB.Client.Node.Get(c.Request.Context(), nodeID); err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "node not found"})
		return
	}
	material, err := nodeauth.GenerateKey()
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "generate api key failed"})
		return
	}
	create := s.DB.Client.NodeAPIKey.Create().
		SetNodeID(nodeID).SetKeyHash(material.Hash).SetKeyPrefix(material.Prefix).
		SetScopes(scopes).SetStatus(config.StatusActive)
	if expires != nil {
		create.SetExpiresAt(*expires)
	}
	created, err := create.Save(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create api key failed"})
		return
	}
	audit(c, "node.key.create", "node_id", nodeID, "key_id", created.ID, "key_prefix", created.KeyPrefix)
	web.JSON(c, http.StatusCreated, gin.H{
		"key":     material.Plaintext,
		"api_key": nodeKeyResponse(created),
	})
}

func (s *Server) handleAdminNodeKeyRevoke(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	nodeID, ok := nodeIDParam(c)
	if !ok {
		return
	}
	keyID, err := ident.Parse(c.Param("keyId"))
	if err != nil || keyID == ident.Nil() {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	key, err := s.DB.Client.NodeAPIKey.Query().
		Where(nodeapikey.ID(keyID), nodeapikey.NodeIDEQ(nodeID)).
		Only(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "api key not found"})
		return
	}
	now := time.Now().UTC()
	updated, err := s.DB.Client.NodeAPIKey.UpdateOneID(key.ID).
		SetStatus("revoked").SetRevokedAt(now).Save(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "revoke api key failed"})
		return
	}
	audit(c, "node.key.revoke", "node_id", nodeID, "key_id", keyID, "key_prefix", key.KeyPrefix)
	web.JSON(c, http.StatusOK, nodeKeyResponse(updated))
}

func (s *Server) handleAdminNodeKeyRotate(c *gin.Context) {
	if !s.requireAdmin(c) {
		return
	}
	nodeID, ok := nodeIDParam(c)
	if !ok {
		return
	}
	keyID, err := ident.Parse(c.Param("keyId"))
	if err != nil || keyID == ident.Nil() {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	limitRequestBody(c, 64<<10)
	var body struct {
		RevokeOld     bool    `json:"revoke_old"`
		ExpiresAt     *string `json:"expires_at"`
		ExpiresInDays *int    `json:"expires_in_days"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	expires, ok := parseKeyExpiry(body.ExpiresAt, body.ExpiresInDays)
	if !ok {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid expiry"})
		return
	}
	old, err := s.DB.Client.NodeAPIKey.Query().
		Where(nodeapikey.ID(keyID), nodeapikey.NodeIDEQ(nodeID)).
		Only(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "api key not found"})
		return
	}
	if old.Status != config.StatusActive {
		web.JSON(c, http.StatusConflict, gin.H{"error": "only active keys can be rotated"})
		return
	}
	material, err := nodeauth.GenerateKey()
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "generate api key failed"})
		return
	}
	create := s.DB.Client.NodeAPIKey.Create().
		SetNodeID(nodeID).SetKeyHash(material.Hash).SetKeyPrefix(material.Prefix).
		SetScopes(old.Scopes).SetStatus(config.StatusActive)
	if expires != nil {
		create.SetExpiresAt(*expires)
	}
	created, err := create.Save(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "rotate api key failed"})
		return
	}
	if body.RevokeOld {
		now := time.Now().UTC()
		if _, err := s.DB.Client.NodeAPIKey.UpdateOneID(old.ID).
			SetStatus("revoked").SetRevokedAt(now).Save(c.Request.Context()); err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "revoke old api key failed"})
			return
		}
	}
	audit(c, "node.key.rotate", "node_id", nodeID, "old_key_id", old.ID, "new_key_id", created.ID, "key_prefix", created.KeyPrefix)
	web.JSON(c, http.StatusCreated, gin.H{
		"key":         material.Plaintext,
		"api_key":     nodeKeyResponse(created),
		"revoked_old": body.RevokeOld,
	})
}

func nodeIDParam(c *gin.Context) (ident.UUID, bool) {
	id, err := ident.Parse(c.Param("id"))
	if err != nil || id == ident.Nil() {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid node id"})
		return ident.Nil(), false
	}
	return id, true
}

// maxKeyExpiryDays caps day-based expiry at ten years so a typo cannot mint a
// key that outlives every sane rotation policy.
const maxKeyExpiryDays = 3650

// parseKeyExpiry resolves the two accepted expiry forms. expires_in_days is
// the UI-facing form (choose a lifetime, not a calendar date); expires_at stays
// for API clients and custom-date UIs and is only consulted when no day count
// was supplied.
func parseKeyExpiry(expiresAt *string, expiresInDays *int) (*time.Time, bool) {
	if expiresInDays != nil {
		if *expiresInDays < 1 || *expiresInDays > maxKeyExpiryDays {
			return nil, false
		}
		t := time.Now().UTC().AddDate(0, 0, *expiresInDays)
		return &t, true
	}
	return parseExpiry(expiresAt)
}

func parseExpiry(raw *string) (*time.Time, bool) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(*raw))
	if err != nil || !t.After(time.Now().UTC()) {
		return nil, false
	}
	return &t, true
}
