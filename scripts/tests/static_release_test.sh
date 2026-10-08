#!/usr/bin/env bash
# Тесты чистых частей static-release.sh: список работ боевого и
# chitalnya-<дата>.build.json. Сети и docker не нужно, curl подставной.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../static-release.sh
source "$TESTS_DIR/../static-release.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

STUB_BODY=""
STUB_RC=0

# install_stub_curl — curl пишет STUB_BODY в файл после -o и выходит с
# STUB_RC. Функцией: тест, которому нужен свой curl, обязан вернуть этот.
install_stub_curl() {
  curl() {
    local out="" prev=""
    for a in "$@"; do
      [[ "$prev" == -o ]] && out="$a"
      prev="$a"
    done
    [[ -n "$out" ]] && printf '%s' "$STUB_BODY" > "$out"
    return "$STUB_RC"
  }
}
install_stub_curl

assert_eq() {
  local expected="$1" got="$2" what="${3:-значение}"
  [[ "$got" == "$expected" ]] || fail "$what расходится
    ожидалось: $expected
    получено:  $got"
}

test_fetch_prod_works_accepts_list() {
  STUB_BODY='[{"id": 49, "title": "т. 6"}, {"id": 50, "title": "т. 7"}]' STUB_RC=0
  ( fetch_prod_works "$TMPROOT/w.json" ) || fail "непустой список боевого отвергнут"
  [[ "$(jq length "$TMPROOT/w.json")" == 2 ]] || fail "список боевого записан не целиком"
}

test_fetch_prod_works_refuses_failure_empty_and_garbage() {
  local body rc
  for case in '[]|0' '|22' '{"message": "нет"}|0' 'не json|0'; do
    body="${case%|*}" rc="${case##*|}"
    STUB_BODY="$body" STUB_RC="$rc"
    if ( fetch_prod_works "$TMPROOT/w.json" ) 2>/dev/null; then
      fail "список боевого «$body» (curl $rc) принят — архив собрался бы не по нему"
    fi
  done
}

test_fetch_prod_works_refuses_truncated_list() {
  STUB_BODY='[{"id": 1}, {"id": 2}]' STUB_RC=0
  if ( PROD_WORKS_LIMIT=2 fetch_prod_works "$TMPROOT/w.json" ) 2>/dev/null; then
    fail "список, упёршийся в потолок limit, принят — хвост каталога пропал бы молча"
  fi
}

test_fetch_apparatus_pages_collects_prod_page_maps() {
  printf '# шапка\n100  # том снят\n145 apparatus  # аппарат снят\n100 apparatus  # том снят целиком — аппарат не нужен\n' > "$TMPROOT/ex.txt"
  local asked="$TMPROOT/asked"
  curl() {
    local out="" prev="" url=""
    for a in "$@"; do
      [[ "$prev" == -o ]] && out="$a"
      [[ "$a" == http* ]] && url="$a"
      prev="$a"
    done
    printf '%s\n' "$url" >> "$asked"
    printf '[{"page_number": 1, "status": "x"}, {"page_number": 7, "status": "y"}]' > "$out"
  }
  ( PROD_API_URL=http://боевой fetch_apparatus_pages "$TMPROOT/ex.txt" "$TMPROOT/pages.json" ) ||
    fail "полосы боевого не собраны"
  assert_eq '{"145":[1,7]}' "$(jq -c . "$TMPROOT/pages.json")" "полосы боевого"
  assert_eq 'http://боевой/api/works/145/page-map' "$(cat "$asked")" "запросы к боевому"
  install_stub_curl
}

test_fetch_apparatus_pages_refuses_failure() {
  printf '145 apparatus\n' > "$TMPROOT/ex.txt"
  STUB_BODY='' STUB_RC=22
  if ( fetch_apparatus_pages "$TMPROOT/ex.txt" "$TMPROOT/pages.json" ) 2>/dev/null; then
    fail "сбой запроса полос боевого принят — сверка с боевым молча не случилась бы"
  fi
}

test_build_json_describes_archive() {
  printf 'zip-данные' > "$TMPROOT/chitalnya-2026-10-07.zip"
  printf '{"catalog_ids":[49,50],"work_ids":[49,50,51]}\n' > "$TMPROOT/built.json"
  local got
  printf '145 apparatus  # снят\n' > "$TMPROOT/ex.txt"
  got="$(build_json 2026-10-07 abc1234 "$TMPROOT/chitalnya-2026-10-07.zip" 23753 "$TMPROOT/built.json" "$TMPROOT/ex.txt")"
  local ex_sha; ex_sha="$(sha256sum "$TMPROOT/ex.txt" | cut -d' ' -f1)"
  local sha md5
  sha="$(sha256sum "$TMPROOT/chitalnya-2026-10-07.zip" | cut -d' ' -f1)"
  md5="$(md5sum "$TMPROOT/chitalnya-2026-10-07.zip" | cut -d' ' -f1)"
  jq -e --arg sha "$sha" --arg md5 "$md5" --arg ex "$ex_sha" '
    .format == 1 and .file == "chitalnya-2026-10-07.zip" and .date == "2026-10-07"
    and .commit == "abc1234" and .size == 16 and .sha256 == $sha and .md5 == $md5
    and .files == 23753 and .works == 2 and .exclude_sha256 == $ex
    and .catalog_ids == [49, 50] and .work_ids == [49, 50, 51]' <<<"$got" >/dev/null ||
    fail "build.json не тот: $got"
}

# Адрес живой читальни для ссылок «эта страница в читальне онлайн» и для
# самопроверки внешних ссылок — это PROD_API_URL без косой в конце. Без него
# cmd/staticsite берёт PUBLIC_BASE_URL локального .env (обычно пусто) и
# архив молча теряет ссылки на сайт.
test_prod_base_trims_slash() {
  assert_eq "https://lib.example.org" "$(PROD_API_URL=https://lib.example.org/ prod_base)" "с косой"
  assert_eq "https://lib.example.org" "$(PROD_API_URL=https://lib.example.org prod_base)" "без косой"
}

test_build_and_check_get_prod_base() {
  local src="$TESTS_DIR/../static-release.sh"
  grep -qE 'staticsite build .*-base "\$\(prod_base\)"' "$src" ||
    fail "сборка не получает -base из PROD_API_URL"
  grep -qE 'staticsite check .*-allow "\$\(prod_base\)/"' "$src" ||
    fail "самопроверка не получает -allow из PROD_API_URL"
}

run_tests
