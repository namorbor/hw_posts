#!/usr/bin/env bash
set -euo pipefail

# Проверка работающего API; создаёт тестового пользователя, пост и комментарий.
BASE_URL="${BASE_URL:-http://localhost:8080}"
BASE_URL="${BASE_URL%/}"
for dependency in curl python3; do
  command -v "$dependency" >/dev/null || { echo "Required: $dependency" >&2; exit 1; }
done
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT
BODY="$WORK_DIR/response.json"
TOKEN=""
SUFFIX="$(date +%s)_$$"
USERNAME="smoke_$SUFFIX"
EMAIL="$USERNAME@example.com"
PASSWORD="$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')"
REGISTRATION="{\"username\":\"$USERNAME\",\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"

call() {
  local expected="$1" method="$2" path="$3" payload="${4:-}" authorize="${5:-}"
  local args=(--silent --show-error --max-time 15 --output "$BODY" --write-out '%{http_code}' --request "$method")
  if [[ -n "$payload" ]]; then
    args+=(--header 'Content-Type: application/json' --data "$payload")
  fi
  if [[ "$authorize" == "auth" ]]; then
    args+=(--header "Authorization: Bearer $TOKEN")
  fi
  local status
  status="$(curl "${args[@]}" "$BASE_URL$path")"
  if [[ "$status" != "$expected" ]]; then
    echo "FAIL: $method $path returned $status, expected $expected" >&2
    exit 1
  fi
  python3 - "$BODY" "$status" <<'PY'
import json, sys
with open(sys.argv[1]) as response:
    data = json.load(response)
if int(sys.argv[2]) >= 400:
    assert isinstance(data, dict) and data.get("error"), "Missing JSON error"
PY
}

value() {
  python3 - "$BODY" "$1" <<'PY'
import json, sys
with open(sys.argv[1]) as response:
    print(json.load(response)[sys.argv[2]])
PY
}

call 200 GET /health
[[ "$(value status)" == "ok" ]]
call 201 POST /register "$REGISTRATION"
USER_ID="$(value id)"
python3 - "$BODY" <<'PY'
import json, sys
with open(sys.argv[1]) as response:
    data = json.load(response)
assert "password" not in data and "password_hash" not in data
PY
call 409 POST /register "$REGISTRATION"
call 200 POST /login "{\"email\":\"$EMAIL\",\"password\":\"$PASSWORD\"}"
TOKEN="$(value token)"
[[ -n "$TOKEN" ]]
echo 'OK: health, registration, login, duplicate user'

POST='{"title":"Smoke test","content":"Проверка создания и чтения поста"}'
call 401 POST /posts "$POST"
call 201 POST /posts "$POST" auth
POST_ID="$(value id)"
[[ "$(value author_id)" == "$USER_ID" ]]
call 200 GET "/posts/$POST_ID"
[[ "$(value title)" == "Smoke test" ]]
call 200 GET /posts
python3 - "$BODY" "$POST_ID" <<'PY'
import json, sys
with open(sys.argv[1]) as response:
    assert any(post["id"] == int(sys.argv[2]) for post in json.load(response))
PY
echo 'OK: create, get and list posts; unauthorized request rejected'

COMMENT='{"text":"Проверка комментария"}'
call 401 POST "/posts/$POST_ID/comments" "$COMMENT"
call 201 POST "/posts/$POST_ID/comments" "$COMMENT" auth
COMMENT_ID="$(value id)"
[[ "$(value post_id)" == "$POST_ID" && "$(value author_id)" == "$USER_ID" ]]
call 200 GET "/posts/$POST_ID/comments"
python3 - "$BODY" "$COMMENT_ID" <<'PY'
import json, sys
with open(sys.argv[1]) as response:
    comments = json.load(response)
assert any(c["id"] == int(sys.argv[2]) and c["text"] == "Проверка комментария" for c in comments)
PY
call 400 POST /posts '{"title":"  ","content":"body"}' auth
call 400 POST "/posts/$POST_ID/comments" '{"text":"  "}' auth
call 404 GET /posts/999999999999
call 404 GET /missing-smoke-route
echo 'OK: comments, input validation and missing resources'
echo 'Smoke test passed.'
