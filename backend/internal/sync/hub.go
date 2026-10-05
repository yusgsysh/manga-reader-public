package sync

import stdsync "sync"

// Hub distributes coalesced change notifications to two subscriber groups:
//
//   - local subscribers (the web/desktop frontend) are notified when data
//     changed for any reason, including changes applied from a remote peer;
//   - peer subscribers (the sync engine and the /api/sync/events SSE
//     handlers) are only notified when the local outbox gained entries, i.e.
//     when there is something a remote peer does not know yet.
//
// Sends are non-blocking: a slow subscriber that misses an intermediate
// notification still observes the latest one because the channel stays
// readable.
type Hub struct {
	mu     stdsync.Mutex
	nextID int
	local  map[int]chan struct{}
	peer   map[int]chan struct{}
}

// NewHub returns an empty notification hub.
func NewHub() *Hub {
	return &Hub{
		local: make(map[int]chan struct{}),
		peer:  make(map[int]chan struct{}),
	}
}

// SubscribeLocal registers a local subscriber. The returned cancel function
// must be called when the subscriber goes away.
func (h *Hub) SubscribeLocal() (<-chan struct{}, func()) {
	return h.subscribe(h.local)
}

// SubscribePeer registers a peer-facing subscriber.
func (h *Hub) SubscribePeer() (<-chan struct{}, func()) {
	return h.subscribe(h.peer)
}

func (h *Hub) subscribe(group map[int]chan struct{}) (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	group[id] = ch
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(group, id)
		h.mu.Unlock()
	}
}

// NotifyLocal wakes local subscribers (frontend cache refresh).
func (h *Hub) NotifyLocal() {
	h.notify(h.local)
}

// NotifyPeer wakes peer subscribers (outbox has entries a peer lacks).
func (h *Hub) NotifyPeer() {
	h.notify(h.peer)
}

func (h *Hub) notify(group map[int]chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, ch := range group {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
