#!/bin/sh
set -e

ANGIE_BACKEND_URL="${ANGIE_BACKEND_URL:-backend:8080}"
export ANGIE_BACKEND_URL

envsubst '${ANGIE_BACKEND_URL}' < /etc/angie/angie.conf.tpl > /etc/angie/angie.conf

exec angie -g "daemon off;"
