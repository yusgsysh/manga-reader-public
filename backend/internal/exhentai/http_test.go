package exhentai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

// sadPandaPage is what ExHentai returns when it rejects the session: an empty
// document whose only content is the sad panda image, which makes head and body
// text empty.
const sadPandaPage = `<html><head></head><body><img src="/img/sadpanda.png"></body></html>`

const validPage = `<html><head><title>ok</title></head><body><div id="gdt">gallery</div></body></html>`

// resetHealGate keeps the package-level retry gate from leaking between tests.
func resetHealGate(t *testing.T) {
	t.Helper()
	healGate.mu.Lock()
	healGate.blockedUntil = time.Time{}
	healGate.mu.Unlock()
	t.Cleanup(func() {
		healGate.mu.Lock()
		healGate.blockedUntil = time.Time{}
		healGate.mu.Unlock()
	})
}

func clientWithCookies(t *testing.T, target string, cookies ...*http.Cookie) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	u, err := url.Parse(target)
	if err != nil {
		t.Fatalf("parse target: %v", err)
	}
	jar.SetCookies(u, cookies)
	return &http.Client{Jar: jar}
}

// A stale igneous cookie makes ExHentai serve the sad panda page even though
// ipb_member_id/ipb_pass_hash are valid. The proxy must drop it and retry, and
// the retry's Set-Cookie must leave a working session in the jar.
func TestFetchDoc_HealsStaleIgneous(t *testing.T) {
	resetHealGate(t)

	var mu sync.Mutex
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ig, err := r.Cookie("igneous")
		mu.Lock()
		if err == nil {
			sent = append(sent, ig.Value)
		} else {
			sent = append(sent, "")
		}
		mu.Unlock()
		if err == nil && ig.Value == "stale" {
			http.SetCookie(w, &http.Cookie{Name: "igneous", Value: "fresh", Path: "/"})
			fmt.Fprint(w, sadPandaPage)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "igneous", Value: "fresh", Path: "/"})
		fmt.Fprint(w, validPage)
	}))
	defer srv.Close()

	client := clientWithCookies(t, srv.URL,
		&http.Cookie{Name: "ipb_member_id", Value: "1"},
		&http.Cookie{Name: "igneous", Value: "stale"},
	)

	doc, err := fetchDoc(context.Background(), client, srv.URL)
	if err != nil {
		t.Fatalf("fetchDoc: %v", err)
	}
	if got := strings.TrimSpace(doc.Find("#gdt").Text()); got != "gallery" {
		t.Errorf("page content = %q, want the retried document", got)
	}
	if len(sent) != 2 {
		t.Fatalf("upstream requests = %d, want 2 (sad panda then heal)", len(sent))
	}
	if sent[0] != "stale" || sent[1] != "" {
		t.Errorf("sent igneous values = %q, want [stale \"\"] (stale cookie dropped)", sent)
	}
	if !hasCookie(client, srv.URL, "igneous") {
		t.Error("retry did not leave the server-issued igneous cookie in the jar")
	}
}

// Without an igneous cookie the sad panda is about the credentials or the IP, so
// retrying would only double upstream traffic.
func TestFetchDoc_SadPandaWithoutIgneousDoesNotRetry(t *testing.T) {
	resetHealGate(t)

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		fmt.Fprint(w, sadPandaPage)
	}))
	defer srv.Close()

	client := clientWithCookies(t, srv.URL, &http.Cookie{Name: "ipb_member_id", Value: "1"})

	if _, err := fetchDoc(context.Background(), client, srv.URL); !errors.Is(err, ErrSadPanda) {
		t.Fatalf("fetchDoc error = %v, want ErrSadPanda", err)
	}
	if requests != 1 {
		t.Errorf("upstream requests = %d, want 1 (no pointless retry)", requests)
	}
}

// When dropping igneous does not help either, the gate must stop the next sad
// panda from retrying again, or a broken session doubles every request.
func TestFetchDoc_HealGateSuppressesRepeatRetries(t *testing.T) {
	resetHealGate(t)

	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.SetCookie(w, &http.Cookie{Name: "igneous", Value: "fresh", Path: "/"})
		fmt.Fprint(w, sadPandaPage)
	}))
	defer srv.Close()

	client := clientWithCookies(t, srv.URL,
		&http.Cookie{Name: "ipb_member_id", Value: "1"},
		&http.Cookie{Name: "igneous", Value: "stale"},
	)

	if _, err := fetchDoc(context.Background(), client, srv.URL); !errors.Is(err, ErrSadPanda) {
		t.Fatalf("first fetchDoc error = %v, want ErrSadPanda", err)
	}
	if requests != 2 {
		t.Fatalf("upstream requests after first fetch = %d, want 2 (heal attempt)", requests)
	}

	if _, err := fetchDoc(context.Background(), client, srv.URL); !errors.Is(err, ErrSadPanda) {
		t.Fatalf("second fetchDoc error = %v, want ErrSadPanda", err)
	}
	if requests != 3 {
		t.Errorf("upstream requests after second fetch = %d, want 3 (gate blocks the retry)", requests)
	}
}

// The torrents view POSTs a form; it must heal the same way.
func TestPostFormDoc_HealsStaleIgneous(t *testing.T) {
	resetHealGate(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ig, err := r.Cookie("igneous"); err == nil && ig.Value == "stale" {
			fmt.Fprint(w, sadPandaPage)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "igneous", Value: "fresh", Path: "/"})
		fmt.Fprint(w, validPage)
	}))
	defer srv.Close()

	client := clientWithCookies(t, srv.URL,
		&http.Cookie{Name: "ipb_member_id", Value: "1"},
		&http.Cookie{Name: "igneous", Value: "stale"},
	)

	if _, err := postFormDoc(context.Background(), client, srv.URL, url.Values{"torrent_info": {"1"}}); err != nil {
		t.Fatalf("postFormDoc: %v", err)
	}
}

func hasCookie(client *http.Client, target, name string) bool {
	u, err := url.Parse(target)
	if err != nil || client.Jar == nil {
		return false
	}
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == name {
			return true
		}
	}
	return false
}
