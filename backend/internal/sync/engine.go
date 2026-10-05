package sync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// pushDebounce coalesces bursts of local writes into one exchange.
	pushDebounce = 2 * time.Second
	// pollFallback wakes the engine when the SSE stream is down or silent.
	pollFallback = 60 * time.Second
	// sseMaxBackoff caps the reconnect delay.
	sseMaxBackoff = 60 * time.Second
	// pushTimeout bounds a single push exchange (payloads are small).
	pushTimeout = 30 * time.Second
)

// Engine is the sync client: it watches the local outbox, pushes changes to
// the configured server, pulls server changes over the same exchange and
// maintains an SSE subscription so server-side changes arrive in near real
// time.
//
// All connections are outbound (the engine runs on the side that can dial the
// peer), which is what makes desktop-behind-NAT ↔ server sync work.
type Engine struct {
	svc      *Service
	reloadCh chan struct{}
	sse      atomic.Bool
	http     *http.Client
}

func newEngine(s *Service) *Engine {
	return &Engine{
		svc:      s,
		reloadCh: make(chan struct{}, 1),
		http:     &http.Client{Timeout: pushTimeout},
	}
}

// Reload asks the engine to re-read its configuration (config PUT).
func (e *Engine) Reload() {
	select {
	case e.reloadCh <- struct{}{}:
	default:
	}
}

func (e *Engine) sseConnected() bool { return e.sse.Load() }

// Run processes sync work until ctx is cancelled.
func (e *Engine) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		cfg, err := e.svc.LoadClientConfig(ctx)
		if err != nil {
			slog.Error("sync: load config failed", "error", err)
			if !e.waitReload(ctx) {
				return
			}
			continue
		}
		if !cfg.Active() {
			e.sse.Store(false)
			if !e.waitReload(ctx) {
				return
			}
			continue
		}
		e.runSession(ctx, cfg)
	}
}

func (e *Engine) waitReload(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-e.reloadCh:
		return true
	}
}

// runSession drives one configuration epoch: SSE subscription + push cycles.
// It returns when the configuration changes or ctx is cancelled.
func (e *Engine) runSession(ctx context.Context, cfg ClientConfig) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan struct{}, 1)
	go e.sseLoop(sctx, cfg, events)

	peerCh, unsubPeer := e.svc.hub.SubscribePeer()
	defer unsubPeer()

	// Catch up immediately after coming online (or after a config change).
	e.pushCycle(sctx, cfg)

	ticker := time.NewTicker(pollFallback)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.sse.Store(false)
			return
		case <-e.reloadCh:
			e.sse.Store(false)
			return
		case <-events:
			e.pushCycle(sctx, cfg)
		case <-peerCh:
			if !e.debounce(sctx, peerCh) {
				return
			}
			e.pushCycle(sctx, cfg)
		case <-ticker.C:
			e.pushCycle(sctx, cfg)
		}
	}
}

// debounce waits out the write burst, draining coalesced signals. It returns
// false when the session was interrupted.
func (e *Engine) debounce(ctx context.Context, peerCh <-chan struct{}) bool {
	deadline := time.NewTimer(pushDebounce)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-e.reloadCh:
			return false
		case <-peerCh:
			// A newer write extended the burst; restart the wait.
			if !deadline.Stop() {
				select {
				case <-deadline.C:
				default:
				}
			}
			deadline.Reset(pushDebounce)
		case <-deadline.C:
			return true
		}
	}
}

// pushCycle exchanges changes with the server: push everything the outbox
// holds beyond last_pushed_id, then apply the server's response.
func (e *Engine) pushCycle(ctx context.Context, cfg ClientConfig) {
	st, err := e.svc.LoadEngineState(ctx)
	if err != nil {
		syncSetError(ctx, e.svc, fmt.Sprintf("load state: %v", err))
		return
	}

	changes, watermark, err := e.buildPushChanges(ctx, st)
	if err != nil {
		syncSetError(ctx, e.svc, fmt.Sprintf("build changes: %v", err))
		return
	}

	req := PushRequest{
		Cursor:   st.Cursor,
		Snapshot: !st.Bootstrapped,
		Changes:  changes,
	}
	resp, err := e.sendPush(ctx, cfg, req)
	if err != nil {
		syncSetError(ctx, e.svc, err.Error())
		return
	}

	if len(resp.Changes) > 0 {
		applied, _, applyErr := ApplyChanges(ctx, e.svc.client, resp.Changes)
		if applyErr != nil {
			syncSetError(ctx, e.svc, fmt.Sprintf("apply: %v", applyErr))
			return
		}
		if applied > 0 {
			e.svc.hub.NotifyLocal()
		}
	}

	now := time.Now().UTC()
	next := EngineState{
		Cursor:       resp.Cursor,
		LastPushedID: watermark,
		Bootstrapped: true,
		LastSyncAt:   &now,
		LastError:    "",
	}
	if err := e.svc.saveEngineState(ctx, next); err != nil {
		syncSetError(ctx, e.svc, fmt.Sprintf("save state: %v", err))
		return
	}
	if err := pruneOutbox(ctx, e.svc.client, watermark); err != nil {
		slog.Warn("sync: prune local outbox failed", "error", err)
	}
}

// buildPushChanges returns the changes to push and the outbox watermark they
// were built from (the new last_pushed_id).
//
// On the first sync the whole local dataset is sent as a snapshot; entries
// already sitting in the outbox are covered by it, so the watermark jumps to
// the current outbox maximum.
func (e *Engine) buildPushChanges(ctx context.Context, st EngineState) ([]Change, int, error) {
	if !st.Bootstrapped {
		changes, err := ExportSnapshot(ctx, e.svc.client)
		if err != nil {
			return nil, 0, err
		}
		maxID, err := maxOutboxID(ctx, e.svc.client)
		if err != nil {
			return nil, 0, err
		}
		return changes, maxID, nil
	}
	return ReadChangesSince(ctx, e.svc.client, st.LastPushedID)
}

func (e *Engine) sendPush(ctx context.Context, cfg ClientConfig, req PushRequest) (PushResponse, error) {
	var resp PushResponse

	body, err := json.Marshal(req)
	if err != nil {
		return resp, fmt.Errorf("encode push request: %w", err)
	}
	url := cfg.ServerURL + "/api/sync/push"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return resp, fmt.Errorf("build push request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set(headerToken, cfg.Token)

	httpResp, err := e.http.Do(httpReq)
	if err != nil {
		return resp, fmt.Errorf("push to %s: %w", cfg.ServerURL, err)
	}
	defer httpResp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(httpResp.Body, 64<<20))
	if err != nil {
		return resp, fmt.Errorf("read push response: %w", err)
	}
	if httpResp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		return resp, fmt.Errorf("push rejected (%d): %s", httpResp.StatusCode, msg)
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		return resp, fmt.Errorf("decode push response: %w", err)
	}
	return resp, nil
}

// sseLoop keeps an SSE subscription to the server alive with exponential
// backoff; every received event wakes the push cycle (which also pulls).
func (e *Engine) sseLoop(ctx context.Context, cfg ClientConfig, events chan<- struct{}) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		connected := e.sseSession(ctx, cfg, events)
		e.sse.Store(false)
		if ctx.Err() != nil {
			return
		}
		if connected {
			backoff = time.Second
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < sseMaxBackoff {
			backoff *= 2
			if backoff > sseMaxBackoff {
				backoff = sseMaxBackoff
			}
		}
	}
}

// sseSession runs one SSE connection until it drops. It reports whether the
// connection was established (HTTP 200).
func (e *Engine) sseSession(ctx context.Context, cfg ClientConfig, events chan<- struct{}) bool {
	url := cfg.ServerURL + "/api/sync/events"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	httpReq.Header.Set(headerToken, cfg.Token)
	httpReq.Header.Set("Accept", "text/event-stream")

	// No timeout: the stream is long-lived (heartbeats keep it fresh).
	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		slog.Debug("sync: sse connect failed", "error", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		slog.Debug("sync: sse rejected", "status", resp.StatusCode, "body", strings.TrimSpace(string(body)))
		return false
	}

	e.sse.Store(true)
	// A successful connection is itself a catch-up trigger.
	select {
	case events <- struct{}{}:
	default:
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			select {
			case events <- struct{}{}:
			default:
			}
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("sync: sse read failed", "error", err)
	}
	return true
}

// syncSetError records a cycle error in state and logs it.
func syncSetError(ctx context.Context, svc *Service, msg string) {
	slog.Warn("sync: cycle failed", "error", msg)
	st, err := svc.LoadEngineState(ctx)
	if err != nil {
		return
	}
	if st.LastError == msg {
		return
	}
	st.LastError = msg
	_ = svc.saveEngineState(ctx, st)
}
