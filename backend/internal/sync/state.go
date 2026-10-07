package sync

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/syncstate"
)

// sync_state keys. Client configuration (client.*) is user-editable via
// /api/sync/config; replication bookkeeping (state.*) is engine-managed.
const (
	keyEnabled      = "client.enabled"
	keyServerURL    = "client.server_url"
	keyToken        = "client.token"
	keyCursor       = "state.cursor"
	keyLastPushedID = "state.last_pushed_id"
	keyBootstrapped = "state.bootstrapped"
	keyLastSyncAt   = "state.last_sync_at"
	keyLastError    = "state.last_error"
)

// ClientConfig is the sync client role configuration (this instance as the
// initiating side).
type ClientConfig struct {
	Enabled   bool   `json:"enabled"`
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

// Active reports whether the engine has enough configuration to run.
func (c ClientConfig) Active() bool {
	return c.Enabled && c.ServerURL != "" && c.Token != ""
}

// EngineState is the persisted replication bookkeeping.
type EngineState struct {
	Cursor       int
	LastPushedID int
	Bootstrapped bool
	LastSyncAt   *time.Time
	LastError    string
}

func (s *Service) getState(ctx context.Context, key string) (string, bool, error) {
	row, err := s.client.SyncState.Query().
		Where(syncstate.Key(key)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read sync state %q: %w", key, err)
	}
	return row.Value, true, nil
}

func (s *Service) setState(ctx context.Context, key, value string) error {
	existing, err := s.client.SyncState.Query().
		Where(syncstate.Key(key)).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("read sync state %q: %w", key, err)
	}
	if existing == nil {
		if _, err := s.client.SyncState.Create().
			SetKey(key).
			SetValue(value).
			Save(ctx); err != nil {
			return fmt.Errorf("write sync state %q: %w", key, err)
		}
		return nil
	}
	if existing.Value == value {
		return nil
	}
	if err := s.client.SyncState.Update().
		Where(syncstate.Key(key)).
		SetValue(value).
		Exec(ctx); err != nil {
		return fmt.Errorf("write sync state %q: %w", key, err)
	}
	return nil
}

// LoadClientConfig reads the persisted client configuration.
func (s *Service) LoadClientConfig(ctx context.Context) (ClientConfig, error) {
	var cfg ClientConfig
	enabledRaw, _, err := s.getState(ctx, keyEnabled)
	if err != nil {
		return cfg, err
	}
	urlRaw, _, err := s.getState(ctx, keyServerURL)
	if err != nil {
		return cfg, err
	}
	tokenRaw, _, err := s.getState(ctx, keyToken)
	if err != nil {
		return cfg, err
	}
	enabled, _ := strconv.ParseBool(enabledRaw)
	cfg.Enabled = enabled
	cfg.ServerURL = strings.TrimRight(strings.TrimSpace(urlRaw), "/")
	cfg.Token = strings.TrimSpace(tokenRaw)
	return cfg, nil
}

// ConfigUpdate is a partial update of the client configuration: nil fields are
// left untouched, an empty token clears it, the mask keeps the stored value.
type ConfigUpdate struct {
	Enabled   *bool   `json:"enabled"`
	ServerURL *string `json:"server_url"`
	Token     *string `json:"token"`
}

// SaveClientConfig applies a partial update and reports the resulting config.
func (s *Service) SaveClientConfig(ctx context.Context, in ConfigUpdate) (ClientConfig, error) {
	current, err := s.LoadClientConfig(ctx)
	if err != nil {
		return current, err
	}

	peerChanged := false
	if in.Enabled != nil {
		current.Enabled = *in.Enabled
	}
	if in.ServerURL != nil {
		u := strings.TrimRight(strings.TrimSpace(*in.ServerURL), "/")
		if u != current.ServerURL {
			// Replication bookkeeping (cursor, outbox watermark, bootstrap
			// flag) is only meaningful for the peer it was collected against.
			// Kept across a peer change it would skip the initial full
			// snapshot: the new peer would receive deltas beyond a cursor it
			// never sent, and its replies would be filtered by the old cursor.
			peerChanged = true
			current.ServerURL = u
		}
	}
	if in.Token != nil {
		t := strings.TrimSpace(*in.Token)
		if t != "" && t != Mask {
			current.Token = t
		} else if t == "" {
			current.Token = ""
		}
		// Mask keeps the stored token unchanged.
	}

	if err := s.setState(ctx, keyEnabled, strconv.FormatBool(current.Enabled)); err != nil {
		return current, err
	}
	if err := s.setState(ctx, keyServerURL, current.ServerURL); err != nil {
		return current, err
	}
	if err := s.setState(ctx, keyToken, current.Token); err != nil {
		return current, err
	}
	if peerChanged {
		if err := s.resetEngineState(ctx); err != nil {
			return current, err
		}
	}
	s.engine.Reload()
	return current, nil
}

// resetEngineState clears the replication bookkeeping so the next exchange
// bootstraps from scratch: the client re-exports a full snapshot and requests
// the peer's full snapshot in the same first push.
func (s *Service) resetEngineState(ctx context.Context) error {
	for _, key := range []string{keyCursor, keyLastPushedID, keyBootstrapped, keyLastSyncAt, keyLastError} {
		if err := s.setState(ctx, key, ""); err != nil {
			return fmt.Errorf("reset sync state %q: %w", key, err)
		}
	}
	return nil
}

// LoadEngineState reads the replication bookkeeping.
func (s *Service) LoadEngineState(ctx context.Context) (EngineState, error) {
	var st EngineState

	cursorRaw, _, err := s.getState(ctx, keyCursor)
	if err != nil {
		return st, err
	}
	pushedRaw, _, err := s.getState(ctx, keyLastPushedID)
	if err != nil {
		return st, err
	}
	bootRaw, _, err := s.getState(ctx, keyBootstrapped)
	if err != nil {
		return st, err
	}
	syncAtRaw, _, err := s.getState(ctx, keyLastSyncAt)
	if err != nil {
		return st, err
	}
	lastErr, _, err := s.getState(ctx, keyLastError)
	if err != nil {
		return st, err
	}

	st.Cursor, _ = strconv.Atoi(cursorRaw)
	st.LastPushedID, _ = strconv.Atoi(pushedRaw)
	st.Bootstrapped, _ = strconv.ParseBool(bootRaw)
	st.LastError = lastErr
	if syncAtRaw != "" {
		if t, parseErr := time.Parse(time.RFC3339Nano, syncAtRaw); parseErr == nil {
			st.LastSyncAt = &t
		}
	}
	return st, nil
}

func (s *Service) saveEngineState(ctx context.Context, st EngineState) error {
	if err := s.setState(ctx, keyCursor, strconv.Itoa(st.Cursor)); err != nil {
		return err
	}
	if err := s.setState(ctx, keyLastPushedID, strconv.Itoa(st.LastPushedID)); err != nil {
		return err
	}
	if err := s.setState(ctx, keyBootstrapped, strconv.FormatBool(st.Bootstrapped)); err != nil {
		return err
	}
	syncAt := ""
	if st.LastSyncAt != nil {
		syncAt = st.LastSyncAt.UTC().Format(time.RFC3339Nano)
	}
	if err := s.setState(ctx, keyLastSyncAt, syncAt); err != nil {
		return err
	}
	return s.setState(ctx, keyLastError, st.LastError)
}
