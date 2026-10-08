#!/usr/bin/env bash
# Каталог экземпляра (instance/) глазами живого nginx: frontend/nginx.conf
# поднимается в nginx:alpine с подложной сборкой и отвечает на настоящие
# запросы. Текстовой сверки директив тут мало — порядок try_files и откат в
# SPA видны только по ответу.
#
# Нужен docker с образом nginx:alpine; без него тест пропускается (SKIP), а
# не проходит молча.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONF="$TESTS_DIR/../../frontend/nginx.conf"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

if ! docker image inspect nginx:alpine >/dev/null 2>&1; then
  echo "SKIP nginx_instance_test: нет docker или образа nginx:alpine"
  exit 0
fi

# Каталог под ~, а не во /tmp: docker этой машины /tmp хозяина не видит.
# ~/.cache есть не везде (раннер CI) — без него mktemp падает до первого теста.
mkdir -p "$HOME/.cache"
WORK="$(mktemp -d "$HOME/.cache/nginx-instance-test.XXXXXX")"
NAME="nginx-instance-test-$$"
cleanup() { docker rm -f "$NAME" >/dev/null 2>&1 || true; rm -rf "$WORK"; }
trap cleanup EXIT

mkdir -p "$WORK/html" "$WORK/instance"
echo '<!doctype html><div id="root"></div>' >"$WORK/html/index.html"
echo 'favicon-сборки' >"$WORK/html/favicon.svg"
echo '{"name":"сборка"}' >"$WORK/html/site.webmanifest"

# start_nginx — поднять nginx с каталогом экземпляра $WORK/instance.
start_nginx() {
  docker rm -f "$NAME" >/dev/null 2>&1 || true
  docker run -d --name "$NAME" --add-host backend:127.0.0.1 \
    -v "$CONF:/etc/nginx/conf.d/default.conf:ro" \
    -v "$WORK/html/index.html:/usr/share/nginx/html/index.html:ro" \
    -v "$WORK/html/favicon.svg:/usr/share/nginx/html/favicon.svg:ro" \
    -v "$WORK/html/site.webmanifest:/usr/share/nginx/html/site.webmanifest:ro" \
    -v "$WORK/instance:/usr/share/nginx/html/instance:ro" \
    nginx:alpine >/dev/null
  local i
  for i in $(seq 1 50); do
    docker exec "$NAME" wget -q -O /dev/null http://127.0.0.1/favicon.svg 2>/dev/null && return 0
    sleep 0.1
  done
  docker logs "$NAME" >&2
  fail "nginx не поднялся"
}

# get PATH — «код|отметка X-Instance-File|тело».
get() {
  # Тело прошлого запроса стираем: на 404 wget файл не пишет. Код берём из
  # строки заголовка ответа (с отступом), а не из «server returned error».
  docker exec "$NAME" sh -c "rm -f /tmp/body; wget -S -q -O /tmp/body http://127.0.0.1$1 2>/tmp/h; code=\$(awk '/^  HTTP\\//{c=\$2} END{print c}' /tmp/h); mark=\$(grep -ci 'x-instance-file' /tmp/h); echo \"\$code|\$mark|\$(cat /tmp/body 2>/dev/null)\""
}

test_empty_instance_serves_build_and_hides_legal() {
  rm -rf "${WORK:?}/instance/"*
  start_nginx
  [[ "$(get /favicon.svg)" == "200|0|favicon-сборки" ]] || fail "иконка без экземпляра: $(get /favicon.svg)"
  [[ "$(get /site.webmanifest)" == '200|0|{"name":"сборка"}' ]] || fail "manifest без экземпляра: $(get /site.webmanifest)"
  local legal; legal="$(get /legal.html)"
  [[ "$legal" == 404* ]] || fail "/legal.html без файла обязан быть 404, а не оболочкой SPA: $legal"
}

test_instance_files_override_build() {
  echo 'favicon-экземпляра' >"$WORK/instance/favicon.svg"
  echo '{"name":"экземпляр"}' >"$WORK/instance/site.webmanifest"
  echo '<h1>право</h1>' >"$WORK/instance/legal.html"
  echo 'ключ' >"$WORK/instance/abc123.txt"
  start_nginx
  [[ "$(get /favicon.svg)" == "200|0|favicon-экземпляра" ]] || fail "иконка экземпляра не взяла верх: $(get /favicon.svg)"
  [[ "$(get /site.webmanifest)" == '200|0|{"name":"экземпляр"}' ]] || fail "manifest экземпляра: $(get /site.webmanifest)"
  [[ "$(get /legal.html)" == "200|1|<h1>право</h1>" ]] || fail "legal.html экземпляра: $(get /legal.html)"
  [[ "$(get /abc123.txt)" == "200|0|ключ" ]] || fail "файл подтверждения поисковика: $(get /abc123.txt)"
}

run_tests
