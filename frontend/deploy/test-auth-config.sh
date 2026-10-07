#!/bin/sh
# Tests for the HTTP Basic Auth plumbing — no containers required.
#
#   1. deploy/generate-auth.sh behaviour matrix (enabled/disabled/credentials)
#   2. static invariants of angie.conf.tpl, docker-entrypoint.sh, Dockerfile,
#      docker-compose.yml, .env.example, .gitignore
#
# Run: sh frontend/deploy/test-auth-config.sh   (needs POSIX sh + htpasswd)

set -u

HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
REPO=$(CDPATH= cd -- "$HERE/../.." && pwd)
GEN="$HERE/generate-auth.sh"
TPL="$HERE/angie.conf.tpl"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

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

# gen <conf-path> <htpasswd-path> [VAR=value ...]
# Runs generate-auth.sh in a scrubbed environment; sets RC / OUT / ERR.
gen() {
    _conf="$1"
    _hp="$2"
    shift 2
    env -i PATH="$PATH" \
        ANGIE_AUTH_CONF="$_conf" \
        ANGIE_HTPASSWD="$_hp" \
        "$@" \
        sh "$GEN" >"$TMP/stdout" 2>"$TMP/stderr"
    RC=$?
    OUT=$(cat "$TMP/stdout")
    ERR=$(cat "$TMP/stderr")
}

has() { # has <file> <fixed-string>
    grep -Fq "$2" "$1"
}

echo "generate-auth.sh"

# --- disabled ---------------------------------------------------------------

gen "$TMP/c1.conf" "$TMP/c1.htpasswd"
if [ $RC -eq 0 ] && [ -f "$TMP/c1.conf" ] && ! grep -q '^auth_basic' "$TMP/c1.conf" && [ ! -e "$TMP/c1.htpasswd" ]; then
    ok "disabled by default (no auth directives, no password file)"
else
    ko "disabled by default" "rc=$RC conf=$( [ -f "$TMP/c1.conf" ] && echo yes || echo no )"
fi

gen "$TMP/c2.conf" "$TMP/c2.htpasswd" MANGA_READER_BASIC_AUTH_ENABLED=false
if [ $RC -eq 0 ] && [ -f "$TMP/c2.conf" ] && ! grep -q '^auth_basic' "$TMP/c2.conf"; then
    ok "MANGA_READER_BASIC_AUTH_ENABLED=false keeps auth off"
else
    ko "MANGA_READER_BASIC_AUTH_ENABLED=false" "rc=$RC"
fi

gen "$TMP/c3.conf" "$TMP/c3.htpasswd" MANGA_READER_BASIC_AUTH_ENABLED=maybe
if [ $RC -ne 0 ] && [ ! -e "$TMP/c3.conf" ]; then
    ok "invalid ENABLED value fails closed (no config written)"
else
    ko "invalid ENABLED value fails closed" "rc=$RC"
fi

# --- enabled via username + password ----------------------------------------

SECRET="hunter2-s3cret"
gen "$TMP/c4.conf" "$TMP/c4.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_USERNAME=admin \
    MANGA_READER_BASIC_AUTH_PASSWORD="$SECRET"
if [ $RC -eq 0 ] &&
    has "$TMP/c4.conf" 'auth_basic "Manga Reader";' &&
    has "$TMP/c4.conf" "auth_basic_user_file \"$TMP/c4.htpasswd\";" &&
    has "$TMP/c4.conf" 'limit_req zone=auth_limit burst=200 nodelay;'; then
    ok "enabled via env credentials writes auth + rate-limit directives"
else
    ko "enabled via env credentials" "rc=$RC conf=$(cat "$TMP/c4.conf" 2>/dev/null)"
fi

if [ -f "$TMP/c4.htpasswd" ] && htpasswd -vb "$TMP/c4.htpasswd" admin "$SECRET" >/dev/null 2>&1; then
    ok "generated htpasswd entry verifies the password"
else
    ko "generated htpasswd entry verifies the password"
fi

if grep -q ':\$apr1\$' "$TMP/c4.htpasswd"; then
    ok 'generated hash is apr1 ($1$) — compatible with Angie/musl'
else
    ko "generated hash is apr1" "$(cat "$TMP/c4.htpasswd" 2>/dev/null)"
fi

if ! grep -Fq "$SECRET" "$TMP/c4.conf" "$TMP/c4.stdout" "$TMP/c4.stderr" 2>/dev/null &&
    ! printf '%s' "$ERR$OUT" | grep -Fq "$SECRET"; then
    ok "password never appears in the include or in output/logs"
else
    ko "password leaked into include or output"
fi

if printf '%s' "$ERR" | grep -qi 'enabled'; then
    ok "logs 'enabled' mode without credentials"
else
    ko "logs enabled mode" "stderr=$ERR"
fi

# --- enabled via existing htpasswd file (docker secret path) ----------------

echo "generate-auth.sh (file source)"
printf 'bob:$apr1$deadbeef$placeholderhash\n' >"$TMP/user.htpasswd"
chmod 644 "$TMP/user.htpasswd"
cp "$TMP/user.htpasswd" "$TMP/user.htpasswd.orig"
gen "$TMP/c5.conf" "$TMP/c5.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_FILE="$TMP/user.htpasswd"
if [ $RC -eq 0 ] &&
    has "$TMP/c5.conf" "auth_basic_user_file \"$TMP/user.htpasswd\";" &&
    [ ! -e "$TMP/c5.htpasswd" ] &&
    cmp -s "$TMP/user.htpasswd" "$TMP/user.htpasswd.orig"; then
    ok "htpasswd file is used as-is (not copied, not regenerated)"
else
    ko "htpasswd file source" "rc=$RC"
fi

gen "$TMP/c6.conf" "$TMP/c6.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_FILE="$TMP/nonexistent.htpasswd"
if [ $RC -ne 0 ]; then
    ok "missing htpasswd file fails closed"
else
    ko "missing htpasswd file fails closed" "rc=$RC"
fi

gen "$TMP/c7.conf" "$TMP/c7.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_FILE="$TMP/user.htpasswd"
chmod 600 "$TMP/user.htpasswd"
gen "$TMP/c7b.conf" "$TMP/c7b.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_FILE="$TMP/user.htpasswd"
if [ $RC -ne 0 ]; then
    ok "world-unreadable htpasswd file (600) fails closed"
else
    ko "world-unreadable htpasswd file fails closed" "rc=$RC"
fi
chmod 644 "$TMP/user.htpasswd"

# --- enabled via password file ----------------------------------------------

echo "generate-auth.sh (password file)"
printf 'line1-pw-from-file\r\nsecond line ignored\n' >"$TMP/pwfile"
gen "$TMP/c8.conf" "$TMP/c8.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_USERNAME=carol \
    MANGA_READER_BASIC_AUTH_PASSWORD_FILE="$TMP/pwfile"
if [ $RC -eq 0 ] && htpasswd -vb "$TMP/c8.htpasswd" carol "line1-pw-from-file" >/dev/null 2>&1; then
    ok "password file: first line used, CR stripped, verifies"
else
    ko "password file" "rc=$RC"
fi

if printf '%s' "$ERR$OUT" | grep -Fq "line1-pw-from-file"; then
    ko "password file content leaked into output"
else
    ok "password file content never appears in output/logs"
fi

# --- missing/invalid credential combinations fail closed ---------------------

echo "generate-auth.sh (fail-closed combinations)"
gen "$TMP/c9.conf" "$TMP/c9.htpasswd" MANGA_READER_BASIC_AUTH_ENABLED=true
[ $RC -ne 0 ] && ok "enabled without any credentials fails" || ko "enabled without credentials" "rc=$RC"

gen "$TMP/c10.conf" "$TMP/c10.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_USERNAME=admin \
    MANGA_READER_BASIC_AUTH_PASSWORD=""
[ $RC -ne 0 ] && ok "enabled with empty password fails" || ko "enabled with empty password" "rc=$RC"

gen "$TMP/c11.conf" "$TMP/c11.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_USERNAME="ad:min" \
    MANGA_READER_BASIC_AUTH_PASSWORD="$SECRET"
[ $RC -ne 0 ] && [ ! -e "$TMP/c11.conf" ] &&
    ok "username containing ':' fails before any config is written" ||
    ko "username containing ':' fails closed" "rc=$RC"

gen "$TMP/c12.conf" "$TMP/c12.htpasswd" \
    MANGA_READER_BASIC_AUTH_ENABLED=true \
    MANGA_READER_BASIC_AUTH_USERNAME=admin
[ $RC -ne 0 ] && ok "enabled without password fails" || ko "enabled without password" "rc=$RC"

# --- angie.conf.tpl invariants ------------------------------------------------

echo "angie.conf.tpl"

if has "$TPL" 'include /etc/angie/basic-auth.conf;'; then
    ok "server includes the generated basic-auth.conf"
else
    ko "include of basic-auth.conf missing"
fi

inc_line=$(grep -n 'include /etc/angie/basic-auth.conf;' "$TPL" | head -1 | cut -d: -f1)
first_loc=$(grep -n '^[[:space:]]*location' "$TPL" | head -1 | cut -d: -f1)
if [ -n "$inc_line" ] && [ -n "$first_loc" ] && [ "$inc_line" -lt "$first_loc" ]; then
    ok "auth include sits at server level (before any location)"
else
    ko "auth include position" "include=$inc_line first_location=$first_loc"
fi

grep -Fq 'auth_basic off;' "$TPL" && grep -A8 'location = /healthz' "$TPL" | grep -Fq 'auth_basic off;' &&
    ok "/healthz opts out of Basic Auth" || ko "/healthz auth_basic off missing"

grep -Fq 'limit_req_zone $binary_remote_addr zone=auth_limit:10m rate=50r/s;' "$TPL" &&
    ok "http-level limit_req_zone declared" || ko "limit_req_zone missing"
grep -Fq 'limit_req_status 429;' "$TPL" &&
    ok "rate-limited requests answer 429 (not 503)" || ko "limit_req_status missing"

grep -Fq 'proxy_set_header Authorization "";' "$TPL" &&
    ok "Authorization header stripped before proxying (backend never sees it)" ||
    ko "Authorization strip missing"

for h in "X-Content-Type-Options nosniff" "X-Frame-Options SAMEORIGIN" "Referrer-Policy strict-origin-when-cross-origin"; do
    grep -Fq "$h" "$TPL" && ok "security header: $h" || ko "security header missing: $h"
done

# Locations with their own add_header lose inherited ones — each must repeat them.
grep -c 'X-Content-Type-Options' "$TPL" | grep -qx '5' &&
    ok "security headers repeated in all 5 header-owning scopes" ||
    ko "unexpected nosniff count" "$(grep -c 'X-Content-Type-Options' "$TPL")"

sse_block=$(grep -A12 -F 'location ~ ^/api/(sync/events|events/changes)$' "$TPL")
printf '%s' "$sse_block" | grep -Fq 'proxy_buffering off;' &&
    printf '%s' "$sse_block" | grep -Fq 'proxy_cache off;' &&
    printf '%s' "$sse_block" | grep -Fq 'gzip off;' &&
    printf '%s' "$sse_block" | grep -Fq 'proxy_read_timeout 3600s;' &&
    ok "SSE location keeps unbuffered/uncached/long-timeout settings" ||
    ko "SSE location settings changed"

printf '%s' "$sse_block" | grep -Fq 'auth_basic' &&
    ko "SSE location overrides auth (must inherit server-level)" ||
    ok "SSE location inherits server-level auth (no override)"

# --- wiring: entrypoint / Dockerfile / compose / .env.example / .gitignore ---

echo "wiring"

grep -Fq 'generate-auth.sh' "$HERE/docker-entrypoint.sh" &&
    ok "entrypoint runs generate-auth.sh before angie starts" ||
    ko "entrypoint does not call generate-auth.sh"

grep -Fq 'apache2-utils' "$REPO/frontend/Dockerfile" &&
    grep -Fq 'COPY deploy/generate-auth.sh' "$REPO/frontend/Dockerfile" &&
    grep -Fq 'COPY deploy/basic-auth.conf' "$REPO/frontend/Dockerfile" &&
    ok "Dockerfile ships htpasswd tool + scripts + default include" ||
    ko "Dockerfile missing auth files/packages"

grep -Fq 'MANGA_READER_BASIC_AUTH_ENABLED: ${MANGA_READER_BASIC_AUTH_ENABLED:-false}' \
    "$REPO/docker-compose.yml" &&
    ok "compose passes Basic Auth env vars to frontend" ||
    ko "compose does not pass Basic Auth env vars"

# The backend must stay private; only the frontend proxy publishes a port.
backend_block=$(awk '/^  backend:/{f=1} /^  frontend:/{f=0} f' "$REPO/docker-compose.yml")
frontend_block=$(awk '/^  frontend:/{f=1} /^volumes:/{f=0} f' "$REPO/docker-compose.yml")
if printf '%s\n' "$backend_block" | grep -E '^[[:space:]]+ports:' >/dev/null; then
    ko "compose publishes a backend port (backend must stay private)"
elif printf '%s\n' "$frontend_block" | grep -Fq '5173:80'; then
    ok "only the frontend proxy publishes a port (backend private)"
else
    ko "frontend does not publish the public 5173:80 entry"
fi

grep -E '^MANGA_READER_BASIC_AUTH_ENABLED=false$' "$REPO/.env.example" >/dev/null &&
    ok ".env.example defaults Basic Auth to false (uncommented)" ||
    ko ".env.example missing default ENABLED=false"

grep -Fq '*.htpasswd' "$REPO/.gitignore" && grep -Fq 'secrets/' "$REPO/.gitignore" &&
    ok ".gitignore blocks *.htpasswd and secrets/" ||
    ko ".gitignore missing credential patterns"

# --- rendered config sanity (envsubst) ---------------------------------------

rendered="$TMP/angie.rendered"
ANGIE_BACKEND_URL='backend:8080' envsubst '${ANGIE_BACKEND_URL}' <"$TPL" >"$rendered"
if grep -Fq 'server backend:8080;' "$rendered" && ! grep -Fq '${' "$rendered"; then
    ok "template renders with envsubst (no unresolved \${...} placeholders)"
else
    ko "template render check failed"
fi

# --- summary ------------------------------------------------------------------

echo
if [ "$fails" -eq 0 ]; then
    echo "test-auth-config: $total/$total passed"
    exit 0
fi
echo "test-auth-config: $fails/$total FAILED"
exit 1
