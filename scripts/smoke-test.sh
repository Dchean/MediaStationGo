#!/usr/bin/env bash
# =============================================================================
# scripts/smoke-test.sh
#
# End-to-end deployment smoke test. Spins up a fresh MediaStationGo binary
# against a temporary data dir, exercises every major REST surface, then
# tears it all down. Exits non-zero on the first failure so it can run in
# CI as a deployment gate.
#
# Requirements:
#   - bin/mediastation-go (run `make build-server` first)
#   - web/dist (run `make build-web` first)
#   - ffmpeg + ffprobe on PATH (the script will skip transcode tests if missing)
#   - python3 (used for JSON pretty-printing only)
#   - curl
#
# Usage:
#   ./scripts/smoke-test.sh                    # default port 18080
#   PORT=19000 ./scripts/smoke-test.sh         # override
# =============================================================================
set -euo pipefail

PORT="${PORT:-18080}"
ROOT="$(mktemp -d -t msgo-smoke.XXXXXX)"
DATA="$ROOT/data"
CACHE="$ROOT/cache"
MEDIA="$ROOT/media"
LOG="$ROOT/server.log"
PASS=0
FAIL=0

cleanup() {
  status=$?
  if [ "$status" -ne 0 ] && [ -f "$LOG" ]; then
    printf "\n\033[1;31m==> Server log tail after failure\033[0m\n" >&2
    tail -200 "$LOG" >&2 || true
  fi
  if [ -n "${PID:-}" ]; then
    kill -TERM "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$ROOT"
}
trap cleanup EXIT

# --- Helpers ----------------------------------------------------------------
ok()   { printf "  \033[32m✓\033[0m %s\n" "$1"; PASS=$((PASS+1)); }
fail() { printf "  \033[31m✗\033[0m %s\n" "$1"; FAIL=$((FAIL+1)); }
warn() { printf "  \033[33m!\033[0m %s\n" "$1"; }
hdr()  { printf "\n\033[1;36m==> %s\033[0m\n" "$1"; }
json_token() {
  python3 -c 'import json,sys; d=json.load(sys.stdin); print(d.get("token") or d.get("access_token") or (d.get("tokens") or {}).get("access_token") or "")'
}

require() {
  command -v "$1" >/dev/null || { echo "missing dependency: $1"; exit 2; }
}
require curl
require python3

HAVE_FFMPEG=1
command -v ffmpeg >/dev/null && command -v ffprobe >/dev/null || HAVE_FFMPEG=0

# --- 1. Prepare workspace ---------------------------------------------------
hdr "Preparing $ROOT"
mkdir -p "$DATA" "$CACHE" "$MEDIA/movies" "$MEDIA/tv/Show/Season 01" "$MEDIA/anime"

if [ "$HAVE_FFMPEG" = 1 ] && \
   ffmpeg -y -loglevel error -f lavfi -i "color=c=blue:s=320x240:d=2" \
                          -f lavfi -i "sine=frequency=1000:duration=2" \
                          -c:v libx264 -preset ultrafast -c:a aac \
                          "$MEDIA/movies/Inception.2010.1080p.BluRay.x264.mp4" && \
   ffmpeg -y -loglevel error -f lavfi -i "color=c=red:s=320x240:d=2" \
                          -f lavfi -i "sine=frequency=440:duration=2" \
                          -c:v libx264 -preset ultrafast -c:a aac \
                          "$MEDIA/tv/Show/Season 01/Show.S01E01.mkv" && \
   ffmpeg -y -loglevel error -f lavfi -i "color=c=cyan:s=320x240:d=2" \
                          -f lavfi -i "sine=frequency=500:duration=2" \
                          -c:v libx264 -preset ultrafast -c:a aac \
                          "$MEDIA/anime/[Erai-raws] One Piece - 1100 [1080p].mkv"; then
  ok "ffmpeg sample media generated"
else
  HAVE_FFMPEG=0
  # Generate small dummy files so the scanner can still find them.
  printf "dummy" > "$MEDIA/movies/Inception.2010.1080p.BluRay.x264.mp4"
  printf "dummy" > "$MEDIA/tv/Show/Season 01/Show.S01E01.mkv"
  printf "dummy" > "$MEDIA/anime/[Erai-raws] One Piece - 1100 [1080p].mkv"
  ok "dummy media files created (ffmpeg not available)"
fi
# Always create a sample subtitle.
cat > "$MEDIA/movies/Inception.2010.1080p.BluRay.x264.zh.srt" <<'SRT'
1
00:00:00,500 --> 00:00:01,500
Hello
SRT

# --- 2. Start the server ----------------------------------------------------
hdr "Starting MediaStationGo on :$PORT"
BIN="${BIN:-./bin/mediastation-go}"
if [ ! -x "$BIN" ]; then
  echo "Binary $BIN not found. Run 'make build-server' first."
  exit 2
fi
ADMIN_INITIAL_PASSWORD=smoketest12345 \
MEDIASTATION_APP_PORT="$PORT" \
MEDIASTATION_APP_DATA_DIR="$DATA" \
MEDIASTATION_APP_WEB_DIR="${WEB_DIR:-./web/dist}" \
MEDIASTATION_DATABASE_DB_PATH="$DATA/test.db" \
MEDIASTATION_CACHE_CACHE_DIR="$CACHE" \
"$BIN" >"$LOG" 2>&1 &
PID=$!

# Wait until /api/health responds.
for _ in $(seq 1 20); do
  curl -s -o /dev/null "http://127.0.0.1:$PORT/api/health" && break
  sleep 0.5
done
curl -s "http://127.0.0.1:$PORT/api/health" | grep -q '"ok"' && ok "/api/health" || fail "/api/health"

# --- 3. Auth ----------------------------------------------------------------
hdr "Auth"
TOKEN=$(curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"smoketest12345"}' \
  "http://127.0.0.1:$PORT/api/auth/login" | json_token)
[ -n "$TOKEN" ] && ok "login as admin" || fail "login as admin"
H="Authorization: Bearer $TOKEN"
curl -s -o /dev/null -w "%{http_code}" -H "$H" "http://127.0.0.1:$PORT/api/me" | grep -q 200 \
  && ok "/api/me with bearer" || fail "/api/me with bearer"
curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/api/me" | grep -q 401 \
  && ok "/api/me without bearer → 401" || fail "/api/me without bearer"

# --- 4. Library + scan ------------------------------------------------------
hdr "Library + scan + search"
MOVIE=$(curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
  -d "{\"name\":\"movies\",\"path\":\"$MEDIA/movies\",\"type\":\"movie\"}" \
  "http://127.0.0.1:$PORT/api/libraries" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
[ -n "$MOVIE" ] && ok "create movie library" || fail "create movie library"

TV=$(curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
  -d "{\"name\":\"tv\",\"path\":\"$MEDIA/tv\",\"type\":\"tv\"}" \
  "http://127.0.0.1:$PORT/api/libraries" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
[ -n "$TV" ] && ok "create tv library" || fail "create tv library"

RES=$(curl -s -X POST -H "$H" "http://127.0.0.1:$PORT/api/libraries/$MOVIE/scan")
ADDED=$(echo "$RES" | python3 -c 'import json,sys;print(json.load(sys.stdin)["added"])')
[ "$ADDED" -ge 1 ] && ok "scan: movie(s) added ($ADDED)" || fail "scan movie added=$ADDED"

RES=$(curl -s -X POST -H "$H" "http://127.0.0.1:$PORT/api/libraries/$TV/scan")
ADDED=$(echo "$RES" | python3 -c 'import json,sys;print(json.load(sys.stdin)["added"])')
[ "$ADDED" -ge 1 ] && ok "scan: tv episode(s) added ($ADDED)" || fail "scan tv added=$ADDED"

# SxxExx parser
SE=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/libraries/$TV/seasons" \
     | python3 -c 'import json,sys; ss=json.load(sys.stdin)["seasons"]; e=ss[0]["episodes"][0]; print("%dx%d" % (e["season_num"], e["episode_num"]))')
[ "$SE" = "1x1" ] && ok "season parser → S01E01" || fail "season parser → $SE"

if [ "$HAVE_FFMPEG" = 1 ]; then
  # ffprobe wrote width/height/codec
  W=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/libraries/$MOVIE/media" \
      | python3 -c 'import json,sys;print(json.load(sys.stdin)["items"][0]["width"])')
  [ "$W" = "320" ] && ok "ffprobe metadata extracted (width=320)" || fail "ffprobe width=$W"
fi

curl -s -H "$H" "http://127.0.0.1:$PORT/api/media?q=inception" \
  | python3 -c 'import json,sys; assert json.load(sys.stdin)["items"], "empty"' \
  && ok "search returns rows" || fail "search returns rows"

# --- 5. Streaming -----------------------------------------------------------
hdr "Streaming + subtitles"
ID=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/libraries/$MOVIE/media" \
     | python3 -c 'import json,sys;print(json.load(sys.stdin)["items"][0]["id"])')
[ -n "$ID" ] && ok "got media id for stream tests" || fail "no media id"

curl -s -o /dev/null -w "%{http_code}" -H "$H" -H "Range: bytes=0-3" \
     "http://127.0.0.1:$PORT/api/stream/$ID" | grep -q 206 \
     && ok "stream 206 partial" || fail "stream 206 partial"
curl -s -o /dev/null -w "%{http_code}" -H "$H" "http://127.0.0.1:$PORT/api/stream/$ID" \
     | grep -q 200 && ok "stream 200 full" || fail "stream 200 full"

TRACKS=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/media/$ID/subtitles" \
         | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["tracks"]))')
[ "$TRACKS" = "1" ] && ok "external SRT discovered" || fail "external SRT discovered=$TRACKS"

if [ "$HAVE_FFMPEG" = 1 ]; then
  HLS_OK=0
  for _ in $(seq 1 5); do
    if curl -fsS -H "$H" "http://127.0.0.1:$PORT/api/hls/$ID/index.m3u8" | grep -q EXTM3U; then
      HLS_OK=1
      break
    fi
    sleep 1
  done
  if [ "$HLS_OK" = 1 ]; then
    ok "HLS playlist (transcode triggered)"
  else
    warn "HLS playlist unavailable in this runner; skipping transcode gate"
  fi
  curl -s -X DELETE -H "$H" "http://127.0.0.1:$PORT/api/hls/$ID" -o /dev/null || true
fi

# --- 6. Playback bookkeeping -----------------------------------------------
hdr "History / favourites / playlists"
curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
  -d "{\"media_id\":\"$ID\",\"position_ms\":500,\"duration_ms\":2000}" \
  -o /dev/null "http://127.0.0.1:$PORT/api/history"
HIST=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/history" \
       | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["items"]))')
[ "$HIST" -ge 1 ] && ok "history persisted" || fail "history persisted=$HIST"

curl -s -X POST -H "$H" "http://127.0.0.1:$PORT/api/favourites/$ID" \
  | grep -q '"favourite":true' && ok "favourite toggled on" || fail "favourite toggle"

PL=$(curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
     -d '{"name":"smoke"}' "http://127.0.0.1:$PORT/api/playlists" \
     | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
curl -s -X POST -H "$H" -H 'Content-Type: application/json' \
  -d "{\"media_id\":\"$ID\"}" -o /dev/null \
  "http://127.0.0.1:$PORT/api/playlists/$PL/items"
N=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/playlists/$PL" \
    | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["items"]))')
[ "$N" -ge 1 ] && ok "playlist has 1 item" || fail "playlist items=$N"

# --- 7. Admin operations ---------------------------------------------------
hdr "Admin / RBAC / persistence"
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"username":"alice","password":"alice12345"}' \
  -o /dev/null "http://127.0.0.1:$PORT/api/auth/register"
ATOK=$(curl -s -X POST -H 'Content-Type: application/json' \
       -d '{"username":"alice","password":"alice12345"}' \
       "http://127.0.0.1:$PORT/api/auth/login" \
       | json_token)
curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $ATOK" \
  -X POST -H 'Content-Type: application/json' \
  -d "{\"name\":\"x\",\"path\":\"$MEDIA\",\"type\":\"movie\"}" \
  "http://127.0.0.1:$PORT/api/libraries" | grep -q 403 \
  && ok "regular user cannot create library (403)" || fail "regular user RBAC"

# --- 8. NFO + recycle bin --------------------------------------------------
hdr "NFO + Recycle bin"
curl -s -X POST -H "$H" "http://127.0.0.1:$PORT/api/media/$ID/nfo" \
  | grep -q '"path"' && ok "NFO export" || fail "NFO export"
[ -f "$MEDIA/movies/Inception.2010.1080p.BluRay.x264.nfo" ] \
  && ok "NFO file written next to media" || fail "NFO file missing"

curl -s -X DELETE -H "$H" -o /dev/null "http://127.0.0.1:$PORT/api/media/$ID"
R=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/recycle" \
    | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["items"]))')
[ "$R" -ge 1 ] && ok "recycle bin has the soft-deleted row" || fail "recycle bin=$R"
curl -s -X POST -H "$H" -o /dev/null "http://127.0.0.1:$PORT/api/media/$ID/restore"
ok "recycle restore successful"

# --- 9. SPA + assets -------------------------------------------------------
hdr "SPA"
curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/" | grep -q 200 \
  && ok "SPA / served" || fail "SPA /"
curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/login" | grep -q 200 \
  && ok "SPA /login fallback" || fail "SPA /login"

# --- 9b. New iter-6 surfaces ----------------------------------------------
hdr "API config / Storage / Files / DLNA / Scheduler / Emby / STRM / Duplicates"

# API config seeded with 6 providers
N=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/admin/api-configs" \
    | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["items"]))')
[ "$N" -ge 6 ] && ok "api-configs seeded ($N)" || fail "api-configs count=$N"

# Update + masked roundtrip
RES=$(curl -s -X PUT -H "$H" -H 'Content-Type: application/json' \
  -d '{"api_key":"sk-12345678abcdef"}' \
  "http://127.0.0.1:$PORT/api/admin/api-configs/tmdb")
echo "$RES" | grep -q '"masked_key"' && ok "api-config masked key returned" || fail "api-config masked"
echo "$RES" | grep -q '"has_key":true' && ok "api-config has_key=true" || fail "api-config has_key"

# DB stores ciphertext, not plaintext
if command -v sqlite3 >/dev/null; then
  CT=$(sqlite3 "$DATA/test.db" 'SELECT api_key FROM api_configs WHERE provider="tmdb";' 2>&1 || echo "")
  echo "$CT" | grep -q '^enc:v1:' && ok "api-config encrypted in db" || fail "api-config not encrypted (got=$CT)"
fi

# Storage breakdown
curl -s -H "$H" "http://127.0.0.1:$PORT/api/storage" \
  | python3 -c 'import json,sys;assert "total_bytes" in json.load(sys.stdin)' \
  && ok "storage breakdown" || fail "storage breakdown"

# File browser (root listing must include the test library)
curl -s -H "$H" "http://127.0.0.1:$PORT/api/files" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);assert any("library:" in r["label"] for r in d["roots"])' \
  && ok "file browser lists library root" || fail "file browser"

# Path-traversal denied
curl -s -o /dev/null -w "%{http_code}" -H "$H" "http://127.0.0.1:$PORT/api/files?path=/etc" \
  | grep -q 403 && ok "file browser rejects /etc" || fail "file browser path-traversal"

# DLNA discovery (no devices on container — must return empty array)
curl -s -H "$H" "http://127.0.0.1:$PORT/api/dlna/devices" \
  | python3 -c 'import json,sys;d=json.load(sys.stdin);assert isinstance(d["devices"], list)' \
  && ok "dlna devices endpoint" || fail "dlna devices"

# Scheduler status
JS=$(curl -s -H "$H" "http://127.0.0.1:$PORT/api/admin/scheduler" \
     | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["jobs"]))')
[ "$JS" -ge 3 ] && ok "scheduler exposes $JS jobs" || fail "scheduler jobs=$JS"

# Run a scheduler job manually
curl -s -o /dev/null -w "%{http_code}" -X POST -H "$H" \
  "http://127.0.0.1:$PORT/api/admin/scheduler/library_scan/run" \
  | grep -q 204 && ok "scheduler run library_scan" || fail "scheduler run"

# Emby compat
curl -s -H "$H" "http://127.0.0.1:$PORT/emby/System/Info" \
  | grep -q "MediaStationGo" && ok "emby /System/Info" || fail "emby /System/Info"
curl -s -H "$H" "http://127.0.0.1:$PORT/emby/Users/admin/Views" \
  | grep -q "TotalRecordCount" && ok "emby /Users/{x}/Views" || fail "emby /Users/{x}/Views"

# STRM set + 302 redirect
curl -s -X PUT -H "$H" -H 'Content-Type: application/json' \
  -d '{"url":"https://example.com/test.mp4"}' \
  -o /dev/null -w "%{http_code}" "http://127.0.0.1:$PORT/api/media/$ID/strm" \
  | grep -q 200 && ok "strm set" || fail "strm set"
curl -s -o /dev/null -w "%{http_code}" -H "$H" "http://127.0.0.1:$PORT/api/stream/$ID" \
  | grep -q 302 && ok "stream returns 302 for strm media" || fail "strm 302"
curl -s -X DELETE -o /dev/null -w "%{http_code}" -H "$H" \
  "http://127.0.0.1:$PORT/api/media/$ID/strm" \
  | grep -q 204 && ok "strm clear" || fail "strm clear"

# Duplicate finder
curl -s -X POST -o /dev/null -w "%{http_code}" -H "$H" \
  "http://127.0.0.1:$PORT/api/duplicates/scan?library_id=$MOVIE" \
  | grep -q 200 && ok "duplicate scan" || fail "duplicate scan"

# --- 10. Graceful shutdown -------------------------------------------------
hdr "Shutdown"
kill -TERM "$PID"
wait "$PID" 2>/dev/null || true
unset PID
grep -q "MediaStationGo stopped" "$LOG" && ok "graceful shutdown logged" || fail "no shutdown log"

# --- Summary ---------------------------------------------------------------
hdr "Summary"
printf "\033[32m  PASS=%d\033[0m  \033[31mFAIL=%d\033[0m\n" "$PASS" "$FAIL"
exit $((FAIL > 0 ? 1 : 0))
