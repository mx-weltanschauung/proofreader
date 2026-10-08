#!/usr/bin/env bash
set -Eeuo pipefail
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"
load_config

TARGET="${1:-$REPO_ROOT/export}"
TARGET="${TARGET%/}"   # a trailing slash (shell tab-completion) would otherwise
                        # make "$TARGET.new" a path *inside* the target dir
API="${API_URL:-http://localhost:8080}"
EMAIL="${ADMIN_EMAIL:-admin@proofreader.local}"
PASSWORD="${ADMIN_PASSWORD:-admin}"

command -v unzip >/dev/null || die "unzip not found — install it (apt install unzip)"

# The login body is assembled by hand, so a quote or backslash in ADMIN_PASSWORD
# would produce invalid JSON. Dev credentials are plain; if that ever changes,
# this needs real JSON encoding.
log "logging in as $EMAIL"
TOKEN="$(curl -fsS -X POST "$API/api/auth/login" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}" \
  | grep -oP '"token":\s*"\K[^"]+')" || die "login failed at $API"
[[ -n "$TOKEN" ]] || die "login returned no token"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

log "downloading export"
curl -fsS -H "Authorization: Bearer $TOKEN" "$API/api/export" -o "$TMP/export.zip" \
  || die "download failed"

# The server cannot report a mid-stream failure: the status is already 200 by
# then, and it deliberately leaves the archive unclosed instead. This check is
# what turns that into a visible error — and it runs before the target is touched.
log "verifying archive"
unzip -tqq "$TMP/export.zip" >/dev/null || die "archive is invalid — the export failed server-side; $TARGET left untouched"

log "unpacking into $TARGET"
# NOTE: this also discards any $TARGET.old left behind by a failed swap below
# (the whole point of leaving it was to preserve the last-known-good tree for
# a human to recover) — by the time export.sh runs again, that tree is gone.
rm -rf "$TARGET.new" "$TARGET.old"
mkdir -p "$TARGET.new"
unzip -qq "$TMP/export.zip" -d "$TARGET.new" || die "unzip into $TARGET.new failed"

# Full replacement, not an unpack on top: a work deleted from the database must
# disappear from the export too, so it shows up in git as a deletion. The swap
# is old-aside, new-in-place, old-removed — never a bare `rm -rf "$TARGET"`
# first. $TARGET itself is never left missing: if it doesn't exist yet there is
# nothing to move aside; otherwise it is renamed to .old before the new tree
# takes its place. If the second `mv` fails, the previous tree survives at
# "$TARGET.old" for manual recovery — die() below says so.
if [[ -e "$TARGET" ]]; then
  mv "$TARGET" "$TARGET.old" || die "cannot move $TARGET aside"
fi
mv "$TARGET.new" "$TARGET" || die "cannot install new export at $TARGET (previous tree kept at $TARGET.old)"
rm -rf "$TARGET.old"

log "export complete: $TARGET"
echo "$TARGET"
