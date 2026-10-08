#!/usr/bin/env bash
# Сторож директив в frontend/nginx.conf. Ни docker, ни сети не нужно.
#
# Дефолты nginx здесь не абстракция: без client_max_body_size тело больше 1 МБ
# отвергается с 413, и загрузка оригинала тома (8–195 МБ) через боевой домен
# невозможна; без proxy_read_timeout синхронная отрисовка превью не укладывается
# в 60 с и возвращает 504, пока сервер продолжает рисовать.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CONF="$TESTS_DIR/../../frontend/nginx.conf"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

# api_block — содержимое блока `location /api/ { … }`, по строке.
api_block() {
  awk '/location \/api\/ \{/{inside=1; next} inside && /^[[:space:]]*\}/{inside=0} inside' "$CONF"
}

test_api_block_exists() {
  [[ -n "$(api_block)" ]] || fail "в $CONF не нашёлся блок location /api/"
}

test_body_limit_is_set_and_large() {
  local line
  line="$(api_block | grep -oP 'client_max_body_size\s+\K[0-9]+[mMgG]' || true)"
  [[ -n "$line" ]] || fail "в location /api/ нет client_max_body_size — тело больше 1 МБ будет отвергнуто с 413"
  local num="${line%[mMgG]}" unit="${line: -1}"
  local mb="$num"
  [[ "$unit" == g || "$unit" == G ]] && mb=$(( num * 1024 ))
  (( mb >= 256 )) || fail "client_max_body_size = $line, а самый тяжёлый оригинал корпуса — 195 МБ"
}

test_proxy_timeouts_are_generous() {
  local read send
  read="$(api_block | grep -oP 'proxy_read_timeout\s+\K[0-9]+' || true)"
  send="$(api_block | grep -oP 'proxy_send_timeout\s+\K[0-9]+' || true)"
  [[ -n "$read" ]] || fail "в location /api/ нет proxy_read_timeout — батч create-pages вернёт 504"
  [[ -n "$send" ]] || fail "в location /api/ нет proxy_send_timeout"
  (( read >= 300 )) || fail "proxy_read_timeout = $read с, для отрисовки превью мало"
  (( send >= 300 )) || fail "proxy_send_timeout = $send с, для заливки оригинала мало"
}

# crawler_map — строка регулярки в map $http_user_agent $is_crawler.
crawler_map() {
  awk '/map \$http_user_agent \$is_crawler/{inside=1; next} inside && /^[[:space:]]*\}/{inside=0} inside' "$CONF"
}

# block <шапка location> — содержимое блока по его первой строке (fixed string).
# Шапка едет через окружение, а не -v: присваивание -v разбирает escape-
# последовательности, и gawk (awk раннеров CI) превращает «\.» в «.» —
# index() искал уже не ту строку. mawk косую оставлял, поэтому локально
# было зелено.
block() {
  BLOCK_HEAD="$1" awk 'index($0, ENVIRON["BLOCK_HEAD"]){inside=1; next} inside && /^[[:space:]]*\}/{inside=0} inside' "$CONF"
}

test_chat_fetchers_are_crawlers() {
  local map; map="$(crawler_map)"
  for ua in ChatGPT-User Claude-User Perplexity-User; do
    grep -q "$ua" <<<"$map" || fail "сборщика $ua нет в \$is_crawler — ссылка на главу в чате отдаст пустую SPA"
  done
}

test_training_bots_are_not_crawlers() {
  local map; map="$(crawler_map)"
  for ua in GPTBot ClaudeBot CCBot Bytespider; do
    ! grep -q "$ua" <<<"$map" || fail "обучающий робот $ua в \$is_crawler — он получит полный текст из /seo"
  done
}

test_md_goes_to_backend_for_everyone() {
  local b; b="$(block 'location ~ ^/works/.+\.md$ {')"
  [[ -n "$b" ]] || fail "нет location для .md под /works/"
  # Переписывание, а не $uri в proxy_pass: proxy_pass с переменной отдаёт
  # $uri декодированным, без повторного экранирования (класс http_splitting у
  # gixy). rewrite…break + proxy_pass без пути экранирует заново —
  # тот же приём, что у location /seo/.
  grep -q 'rewrite ^ /seo$uri break;' <<<"$b" || fail ".md не переписывается в /seo"
  grep -qE 'proxy_pass http://backend:8080;' <<<"$b" || fail ".md не проксируется в бэкенд без пути"
  ! grep -v '^[[:space:]]*#' <<<"$b" | grep -q 'proxy_pass .*\$' || fail ".md проксируется адресом с переменной — декодированный \$uri уедет без экранирования"
  ! grep -q 'is_crawler' <<<"$b" || fail ".md зависит от агента — человек не увидит того, что видит модель"
  ! grep -q 'proxy_intercept_errors' <<<"$b" || fail "404/410 текста уведены в SPA"
}

test_concept_md_goes_to_backend_for_everyone() {
  local b head
  for head in 'location = /concepts.md {' 'location ~ ^/concepts/.+\.md$ {'; do
    b="$(block "$head")"
    [[ -n "$b" ]] || fail "нет $head — .md понятия отдаётся оболочкой SPA"
    grep -q 'rewrite ^ /seo$uri break;' <<<"$b" || fail "$head: не переписывается в /seo"
    grep -qE 'proxy_pass http://backend:8080;' <<<"$b" || fail "$head: не проксируется в бэкенд без пути"
    ! grep -q 'is_crawler' <<<"$b" || fail "$head: зависит от агента"
    ! grep -q 'proxy_intercept_errors' <<<"$b" || fail "$head: ошибки уводятся в SPA"
  done
}

test_llms_txt_goes_to_backend() {
  local b; b="$(block 'location = /llms.txt {')"
  [[ -n "$b" ]] || fail "нет location = /llms.txt — отдаётся оболочка SPA"
  grep -q 'proxy_pass http://backend:8080/seo/llms.txt;' <<<"$b" || fail "/llms.txt не проксируется в бэкенд"
}

test_mcp_goes_to_backend_for_everyone() {
  local b; b="$(block 'location = /mcp {')"
  [[ -n "$b" ]] || fail "нет location = /mcp — запрос нейросети получит оболочку SPA"
  grep -qE 'proxy_pass http://backend:8080;' <<<"$b" || fail "/mcp не проксируется в бэкенд"
  ! grep -q 'is_crawler' <<<"$b" || fail "/mcp зависит от агента"
  ! grep -q 'proxy_intercept_errors' <<<"$b" || fail "ошибки /mcp уведены в SPA"
  grep -qE 'client_max_body_size\s+64k;' <<<"$b" || fail "/mcp без потолка тела 64k"
  local t; t="$(grep -oP 'proxy_read_timeout\s+\K[0-9]+' <<<"$b" || true)"
  [[ -n "$t" && "$t" -ge 120 ]] || fail "/mcp: proxy_read_timeout меньше 120s — ожидание ворот и рендер главы не уложатся"
}

run_tests
