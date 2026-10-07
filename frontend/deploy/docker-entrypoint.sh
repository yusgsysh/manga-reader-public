#!/bin/sh
set -e

ANGIE_BACKEND_URL="${ANGIE_BACKEND_URL:-backend:8080}"
export ANGIE_BACKEND_URL

# HTTP Basic Auth include (comment-only when disabled, auth directives +
# rate limit when enabled). Fails closed: a bad auth configuration stops
# the container here, before angie starts.
/usr/local/bin/generate-auth.sh

envsubst '${ANGIE_BACKEND_URL}' < /etc/angie/angie.conf.tpl > /etc/angie/angie.conf

exec angie -g "daemon off;"
