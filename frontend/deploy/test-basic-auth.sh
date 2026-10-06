#!/bin/sh
# HTTP Basic Auth end-to-end tests against the real rendered Angie config.
#
# Requirements: docker OR podman, go, curl.
# Starts a real backend (go build) unless E2E_BACKEND_URL is provided,
# builds/serves the frontend image, and asserts the public-facing behaviour:
#
#   auth on : 401 + WWW-Authenticate for /, /api/*, SSE — correct creds 200,
#              /healthz exempt, security headers, Authorization stripped
#              before the backend, rendered config contains auth + rate limit,
#              password absent from container logs
#   auth off: original anonymous behaviour restored
#
# Run: sh frontend/deploy/test-basic-auth.sh
#   E2E_SKIP_BUILD=1          reuse an existing image (faster)
#   E2E_IMAGE=<tag>           image tag (default manga-reader-frontend:authtest)
#   E2E_BACKEND_URL=host:port use a running backend instead of building one
#   E2E_HEARTBEAT=0           skip the ~25s SSE heartbeat wait

set -u

HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO=$(CDPATH= cd -- "$HERE/../.." && pwd)
E2E_IMAGE="${E2E_IMAGE:-manga-reader-frontend:authtest}"
USER_NAME="admin"
USER_PW='E2e-Sup3r-S3cret!'
SYNC_TOKEN="authtest-sync-token"
C1=""
C2=""
C3=""

if [ -n "${CONTAINER_CMD:-}" ]; then
    ENGINE="$CONTAINER_CMD"
elif command -v docker >/dev/null 2>&1; then
    ENGINE=docker
elif command -v podman >/dev/null 2>&1; then
    ENGINE=podman
else
    echo "test-basic-auth: need docker or podman (or set CONTAINER_CMD)" >&2
    exit 2
fi

command -v curl >/dev/null 2>&1 || { echo "test-basic-auth: curl required" >&2; exit 2; }
command -v go >/dev/null 2>&1 || [ -n "${E2E_BACKEND_URL:-}" ] || {
    echo "test-basic-auth: go required (or set E2E_BACKEND_URL)" >&2
    exit 2
}

TMP=$(mktemp -d)
BACKEND_PID=""
ECHO_PID=""

# Unique per-run ports and container names: a previously aborted run must
# not race this one for a port or a container name.
RUN_ID=$(date +%s)
FE_PORT=$((20000 + RUN_ID % 20000))
FE2_PORT=$((FE_PORT + 1))
BACKEND_PORT="${E2E_BACKEND_PORT:-$((FE_PORT + 2))}"
ECHO_PORT=$((FE_PORT + 3))
C1="mr-authtest-$RUN_ID-basic"
C2="mr-authtest-$RUN_ID-echo"
C3="mr-authtest-$RUN_ID-off"

cleanup() {
    "$ENGINE" rm -f "$C1" "$C2" "$C3" >/dev/null 2>&1 || true
    [ -n "$BACKEND_PID" ] && kill "$BACKEND_PID" >/dev/null 2>&1 || true
    [ -n "$ECHO_PID" ] && kill "$ECHO_PID" >/dev/null 2>&1 || true
    [ -n "${TMP:-}" ] && rm -rf "$TMP"
}
trap cleanup EXIT INT TERM

# Sweep leftovers from aborted runs (containers + orphaned test binaries).
stale=$("$ENGINE" ps -aq --filter "name=mr-authtest-" 2>/dev/null || true)
[ -n "$stale" ] && "$ENGINE" rm -f $stale >/dev/null 2>&1 || true
pkill -f "backend-bin" >/dev/null 2>&1 || true
pkill -f "echo-bin" >/dev/null 2>&1 || true
sleep 1

fails=0
total=0

ok() {
    total=$((total + 1))
    printf '  ok   %s\n' "$1"
}

ko() {
    total=$((total + 1))
    fails=$((fails + 1))
    printf '  FAIL %s%s\n' "$1" "${2:+ — $2}"
}

die() {
    echo "test-basic-auth: $*" >&2
    exit 2
}

# Container -> host networking: both engines expose the host through a
# gateway alias (docker: host.docker.internal, podman: host.containers.internal).
if [ "$ENGINE" = docker ]; then
    NET_FLAGS="--add-host=host.docker.internal:host-gateway"
    BACKEND_HOST="host.docker.internal"
else
    NET_FLAGS="--add-host=host.containers.internal:host-gateway"
    BACKEND_HOST="host.containers.internal"
fi

# http <url> [curl opts...] -> CODE / BODY / HDRS
http() {
    _url="$1"
    shift
    CODE=$(curl -sS -o "$TMP/body" -D "$TMP/hdrs" -w '%{http_code}' \
        --max-time 20 "$@" "$_url" 2>"$TMP/curlerr" || true)
    [ -n "$CODE" ] || CODE=000
    BODY=$(cat "$TMP/body" 2>/dev/null || true)
    HDRS=$(tr -d '\r' <"$TMP/hdrs" 2>/dev/null || true)
}

# sse <max-time> <url> [curl opts...] -> SSE_OUT
sse() {
    _t="$1"
    _url="$2"
    shift 2
    SSE_OUT=$(curl -sN --max-time "$_t" "$@" "$_url" 2>>"$TMP/curlerr" || true)
}

wait_http() {
    _wurl="$1"
    _i=0
    while [ "$_i" -lt 120 ]; do
        _c=$(curl -s -o /dev/null -w '%{http_code}' --max-time 2 "$_wurl" 2>/dev/null || echo 000)
        [ "$_c" != "000" ] && return 0
        sleep 0.5
        _i=$((_i + 1))
    done
    return 1
}

hdr() { # hdr <name> -> value from last http()
    printf '%s' "$HDRS" | tr '[:upper:]' '[:lower:]' | grep -i "^$1:" | head -1 | cut -d: -f2- | sed 's/^ *//'
}

# ---------------------------------------------------------------- setup -----

echo "engine: $ENGINE"

if [ -z "${E2E_SKIP_BUILD:-}" ]; then
    echo "building $E2E_IMAGE ..."
    "$ENGINE" build -t "$E2E_IMAGE" "$REPO/frontend" >/dev/null ||
        die "image build failed"
else
    echo "reusing existing image $E2E_IMAGE"
fi

if [ -z "${E2E_BACKEND_URL:-}" ]; then
    echo "building backend test binary ..."
    (cd "$REPO/backend" && go build -o "$TMP/backend-bin" .) || die "backend build failed"
    EHENTAI_PORT=":$BACKEND_PORT" \
        MANGA_READER_DB_PATH="$TMP/manga-reader.db" \
        EHENTAI_COOKIE="ipb_member_id=1; ipb_pass_hash=e2etest" \
        MANGA_READER_SYNC_TOKEN="$SYNC_TOKEN" \
        "$TMP/backend-bin" >"$TMP/backend.log" 2>&1 &
    BACKEND_PID=$!
    BACKEND_URL="$BACKEND_HOST:$BACKEND_PORT"
    wait_http "http://127.0.0.1:$BACKEND_PORT/healthz" || {
        cat "$TMP/backend.log"
        die "backend did not start"
    }
else
    BACKEND_URL="$E2E_BACKEND_URL"
fi

# Echo upstream: reveals which headers actually reach the backend.
mkdir -p "$TMP/echosrv"
cat >"$TMP/echosrv/main.go" <<'EOF'
package main

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "{\"authorization\":%q,\"x_forwarded_for\":%q,\"x_real_ip\":%q,\"host\":%q}\n",
			r.Header.Get("Authorization"), r.Header.Get("X-Forwarded-For"),
			r.Header.Get("X-Real-IP"), r.Host)
	})
	http.ListenAndServe(":"+os.Getenv("PORT"), nil)
}
EOF
(cd "$TMP/echosrv" && go mod init echosrv >/dev/null 2>&1; go build -o "$TMP/echo-bin" .) || die "echo build failed"
PORT="$ECHO_PORT" "$TMP/echo-bin" >"$TMP/echo.log" 2>&1 &
ECHO_PID=$!
wait_http "http://127.0.0.1:$ECHO_PORT/" || die "echo server did not start"

start_fe() { # start_fe <container> <host-port> <backend-url> <extra -e args...>
    _c="$1"
    _p="$2"
    _b="$3"
    shift 3
    "$ENGINE" rm -f "$_c" >/dev/null 2>&1 || true
    "$ENGINE" run -d --rm --name "$_c" \
        -p "127.0.0.1:$_p:80" \
        $NET_FLAGS \
        -e "ANGIE_BACKEND_URL=$_b" \
        "$@" \
        "$E2E_IMAGE" >/dev/null || die "container start failed: $_c"
    wait_http "http://127.0.0.1:$_p/healthz" || {
        "$ENGINE" logs "$_c" 2>&1 | tail -20
        die "container did not answer /healthz: $_c (auth module missing?)"
    }
}

# ----------------------------------------------------- auth ENABLED phase ---

echo "phase: MANGA_READER_BASIC_AUTH_ENABLED=true"

start_fe "$C1" "$FE_PORT" "$BACKEND_URL" \
    -e MANGA_READER_BASIC_AUTH_ENABLED=true \
    -e "MANGA_READER_BASIC_AUTH_USERNAME=$USER_NAME" \
    -e "MANGA_READER_BASIC_AUTH_PASSWORD=$USER_PW"
BASE="http://127.0.0.1:$FE_PORT"

# 1. anonymous / -> 401 + realm
http "$BASE/"
if [ "$CODE" = 401 ] && printf '%s' "$HDRS" | grep -qi 'www-authenticate: *basic'; then
    ok "GET / without credentials -> 401 + WWW-Authenticate: Basic"
else
    ko "GET / without credentials" "code=$CODE hdr=$(hdr www-authenticate) curl=$(cat "$TMP/curlerr" 2>/dev/null)"
fi
if printf '%s' "$HDRS" | grep -q 'realm="Manga Reader"'; then
    ok 'realm is "Manga Reader"'
else
    ko "realm value" "$(hdr www-authenticate)"
fi

# 2. wrong password / wrong user -> 401
http "$BASE/" -u "$USER_NAME:wrong-pass"
[ "$CODE" = 401 ] && ok "wrong password -> 401" || ko "wrong password" "code=$CODE"
http "$BASE/" -u "nobody:$USER_PW"
[ "$CODE" = 401 ] && ok "wrong username -> 401" || ko "wrong username" "code=$CODE"

# 3. correct credentials -> 200 HTML shell
http "$BASE/" -u "$USER_NAME:$USER_PW"
if [ "$CODE" = 200 ] && printf '%s' "$BODY" | grep -qi '<html'; then
    ok "correct credentials -> 200 HTML shell"
else
    ko "correct credentials -> 200" "code=$CODE"
fi

# 4. security headers on authenticated response
if printf '%s' "$HDRS" | grep -qi 'x-content-type-options: *nosniff' &&
    printf '%s' "$HDRS" | grep -qi 'x-frame-options: *SAMEORIGIN' &&
    printf '%s' "$HDRS" | grep -qi 'referrer-policy: *strict-origin-when-cross-origin'; then
    ok "security headers present (nosniff / SAMEORIGIN / referrer-policy)"
else
    ko "security headers" "$(printf '%s' "$HDRS" | grep -ci x-content-type-options)"
fi

# 5. API endpoints challenge too
http "$BASE/api/bookshelf"
[ "$CODE" = 401 ] && ok "GET /api/bookshelf anonymous -> 401" || ko "/api/bookshelf anonymous" "code=$CODE"
http "$BASE/api/prefill"
[ "$CODE" = 401 ] && ok "GET /api/prefill anonymous -> 401" || ko "/api/prefill anonymous" "code=$CODE"

# 6. with credentials the real backend answers
http "$BASE/api/bookshelf" -u "$USER_NAME:$USER_PW"
[ "$CODE" = 200 ] && ok "GET /api/bookshelf authenticated -> 200" || ko "/api/bookshelf authenticated" "code=$CODE"
http "$BASE/api/prefill" -u "$USER_NAME:$USER_PW"
[ "$CODE" = 200 ] && ok "GET /api/prefill authenticated -> 200" || ko "/api/prefill authenticated" "code=$CODE"

# 7. healthz is exempt (no credentials, static ok, no sensitive data)
http "$BASE/healthz"
if [ "$CODE" = 200 ] && [ "$BODY" = "ok" ]; then
    ok "GET /healthz without credentials -> 200 static ok"
else
    ko "GET /healthz" "code=$CODE body=$BODY"
fi
if printf '%s' "$BODY" | grep -Eqi 'cookie|token|password|postgres|minio|secret'; then
    ko "/healthz leaks something" "$BODY"
else
    ok "/healthz body carries no sensitive data"
fi

# 8. SSE endpoints: challenge without creds, stream with creds
http "$BASE/api/sync/events"
[ "$CODE" = 401 ] && ok "GET /api/sync/events anonymous -> 401 (SSE behind auth)" ||
    ko "/api/sync/events anonymous" "code=$CODE"

http "$BASE/api/sync/events" -u "$USER_NAME:$USER_PW" -H "X-Sync-Token: wrong-token"
if [ "$CODE" = 401 ] &&
    printf '%s' "$BODY" | grep -q 'sync token' &&
    ! printf '%s' "$HDRS" | grep -qi 'www-authenticate'; then
    ok "Basic passed, sync token still enforced at the app layer"
else
    ko "sync token layering" "code=$CODE body=$BODY"
fi

sse 6 "$BASE/api/sync/events" -u "$USER_NAME:$USER_PW" -H "X-Sync-Token: $SYNC_TOKEN"
if printf '%s' "$SSE_OUT" | grep -q '^data:'; then
    ok "authenticated SSE /api/sync/events streams immediately (proxy_buffering off)"
else
    ko "authenticated SSE stream" "out=$(printf '%s' "$SSE_OUT" | head -c 120)"
fi
# reconnect
sse 6 "$BASE/api/sync/events" -u "$USER_NAME:$USER_PW" -H "X-Sync-Token: $SYNC_TOKEN"
if printf '%s' "$SSE_OUT" | grep -q '^data:'; then
    ok "SSE reconnect with credentials succeeds again"
else
    ko "SSE reconnect"
fi

sse 6 "$BASE/api/events/changes" -u "$USER_NAME:$USER_PW"
if printf '%s' "$SSE_OUT" | grep -q '^data:'; then
    ok "authenticated SSE /api/events/changes streams immediately"
else
    ko "authenticated /api/events/changes" "out=$(printf '%s' "$SSE_OUT" | head -c 120)"
fi

if [ "${E2E_HEARTBEAT:-1}" != 0 ]; then
    sse 32 "$BASE/api/sync/events" -u "$USER_NAME:$USER_PW" -H "X-Sync-Token: $SYNC_TOKEN"
    if printf '%s' "$SSE_OUT" | grep -q ': ping'; then
        ok "SSE heartbeat (: ping, 25s) passes through the proxy"
    else
        ko "SSE heartbeat" "out=$(printf '%s' "$SSE_OUT" | tail -c 120)"
    fi
fi

# 9. image / zip endpoints are auth-gated (status itself depends on ExHentai)
http "$BASE/api/image/thumbnail?url=https://exhentai.org/t/1.jpg" -u "$USER_NAME:$USER_PW"
if ! printf '%s' "$HDRS" | grep -qi 'www-authenticate'; then
    ok "image proxy with credentials is not challenged by Basic Auth"
else
    ko "image proxy challenged unexpectedly" "code=$CODE"
fi

# 10. Authorization header never reaches the backend (echo upstream; must be
#     an /api/* path — the / location serves the SPA shell).
start_fe "$C2" "$FE2_PORT" "$BACKEND_HOST:$ECHO_PORT" \
    -e MANGA_READER_BASIC_AUTH_ENABLED=true \
    -e "MANGA_READER_BASIC_AUTH_USERNAME=$USER_NAME" \
    -e "MANGA_READER_BASIC_AUTH_PASSWORD=$USER_PW"
http "http://127.0.0.1:$FE2_PORT/api/headers" -u "$USER_NAME:$USER_PW"
if [ "$CODE" = 200 ] && printf '%s' "$BODY" | grep -q '"authorization":""' &&
    ! printf '%s' "$BODY" | grep -Fq "$USER_PW"; then
    ok "Authorization stripped before proxying (backend sees an empty header)"
else
    ko "Authorization strip at proxy" "code=$CODE body=$BODY"
fi

# 11. rendered config: auth + rate limit live (the include is inlined at
#     config load, so read the generated file itself; angie -t below proves
#     the include resolves and every directive is accepted).
authcfg=$("$ENGINE" exec "$C1" cat /etc/angie/basic-auth.conf 2>/dev/null)
maincfg=$("$ENGINE" exec "$C1" cat /etc/angie/angie.conf 2>/dev/null)
if printf '%s' "$authcfg" | grep -q '^auth_basic "Manga Reader";' &&
    printf '%s' "$authcfg" | grep -q 'limit_req zone=auth_limit' &&
    printf '%s' "$maincfg" | grep -q 'include /etc/angie/basic-auth.conf;'; then
    ok "generated basic-auth.conf has auth_basic + limit_req, angie.conf includes it"
else
    ko "generated auth config" "auth=$(printf '%s' "$authcfg" | head -3)"
fi
"$ENGINE" exec "$C1" angie -t -c /etc/angie/angie.conf >/dev/null 2>&1 &&
    ok "angie -t validates the rendered config" || ko "angie -t failed"

# 12. logs never contain the password
if "$ENGINE" logs "$C1" 2>&1 | grep -Fq "$USER_PW"; then
    ko "password leaked into container logs"
else
    ok "password absent from container logs"
fi

# ------------------------------------------------------ auth DISABLED phase --

echo "phase: MANGA_READER_BASIC_AUTH_ENABLED=false"

# C1 still holds the shared host port — stop it first (--rm removes it).
"$ENGINE" stop "$C1" >/dev/null 2>&1 || true
"$ENGINE" stop "$C2" >/dev/null 2>&1 || true

start_fe "$C3" "$FE_PORT" "$BACKEND_URL" \
    -e MANGA_READER_BASIC_AUTH_ENABLED=false
BASE="http://127.0.0.1:$FE_PORT"

http "$BASE/"
if [ "$CODE" = 200 ] && ! printf '%s' "$HDRS" | grep -qi 'www-authenticate'; then
    ok "disabled: GET / anonymous -> 200, no challenge"
else
    ko "disabled: GET / anonymous" "code=$CODE hdr=$(hdr www-authenticate)"
fi
http "$BASE/api/bookshelf"
[ "$CODE" = 200 ] && ok "disabled: GET /api/bookshelf anonymous -> 200" ||
    ko "disabled: /api/bookshelf" "code=$CODE"
http "$BASE/healthz"
[ "$CODE" = 200 ] && ok "disabled: GET /healthz -> 200" || ko "disabled: /healthz" "code=$CODE"
http "$BASE/api/sync/events"
if [ "$CODE" = 401 ] && ! printf '%s' "$HDRS" | grep -qi 'www-authenticate'; then
    ok "disabled: sync events only 401 on app-level token (no Basic challenge)"
else
    ko "disabled: sync events" "code=$CODE hdr=$(hdr www-authenticate)"
fi

cfg=$("$ENGINE" exec "$C3" cat /etc/angie/basic-auth.conf 2>/dev/null)
if ! printf '%s' "$cfg" | grep -q '^auth_basic'; then
    ok "disabled: basic-auth.conf carries no auth directives"
else
    ko "disabled: basic-auth.conf still has auth_basic"
fi

# ---------------------------------------------------------------- summary ----

echo
if [ "$fails" -eq 0 ]; then
    echo "test-basic-auth: $total/$total passed"
    exit 0
fi
echo "test-basic-auth: $fails/$total FAILED"
exit 1
