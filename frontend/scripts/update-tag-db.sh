#!/usr/bin/env bash
#
# Update the bundled EhTagTranslation tag translation database.
# Downloads the latest db.text.js from GitHub Release into public/.
#
# Usage:
#   bash scripts/update-tag-db.sh
#
# Environment overrides:
#   VITE_TAG_TRANSLATION_REMOTE_URL  custom download URL (e.g. an intranet mirror)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUTPUT_DIR="${SCRIPT_DIR}/../public"
OUTPUT="${OUTPUT_DIR}/db.text.js"
URL="${VITE_TAG_TRANSLATION_REMOTE_URL:-https://github.com/EhTagTranslation/Database/releases/latest/download/db.text.js}"

mkdir -p "${OUTPUT_DIR}"

echo "Downloading latest EhTagTranslation database..."
echo "  URL: ${URL}"
echo "  -> ${OUTPUT}"

if ! curl -fL "${URL}" -o "${OUTPUT}.tmp"; then
  echo "Error: download failed (HTTP error)." >&2
  rm -f "${OUTPUT}.tmp"
  exit 1
fi

# Sanity check: the JSONP file must reference the expected callback.
if ! grep -q "load_ehtagtranslation_db_text" "${OUTPUT}.tmp"; then
  echo "Error: downloaded file is not a valid db.text.js (callback not found)." >&2
  rm -f "${OUTPUT}.tmp"
  exit 1
fi

# Skip if unchanged, to avoid unnecessary git churn.
if [ -f "${OUTPUT}" ] && cmp -s "${OUTPUT}" "${OUTPUT}.tmp"; then
  echo "Database is already up to date. Nothing to do."
  rm -f "${OUTPUT}.tmp"
  exit 0
fi

mv "${OUTPUT}.tmp" "${OUTPUT}"

SIZE="$(du -h "${OUTPUT}" | cut -f1)"
echo "Done: ${OUTPUT} (${SIZE})"
echo "Restart the dev server or rebuild to pick up the new database."
