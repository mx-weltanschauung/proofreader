#!/usr/bin/env bash
# Тесты карты полос боевого для static-release.sh: merge_page_maps (чистая
# функция) и fetch_work_pages с подставным curl. Сети нет.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../static-release.sh
source "$TESTS_DIR/../static-release.sh"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"

PROD_API_URL=https://prod.example

assert_eq() {
  local expected="$1" got="$2" what="${3:-значение}"
  [[ "$got" == "$expected" ]] || fail "$what расходится
    ожидалось: $expected
    получено:  $got"
}

test_merge_page_maps_two_responses() {
  printf '[{"page_number": 1, "status": "вычитана"}, {"page_number": 3, "status": "вычитана"}]' > "$TMPROOT/a.json"
  printf '[{"page_number": 7, "status": "не_вычитана"}]' > "$TMPROOT/b.json"
  assert_eq '{"49":[1,3],"50":[7]}' "$(merge_page_maps 49 "$TMPROOT/a.json" 50 "$TMPROOT/b.json")" "слияние карт"
}

test_merge_page_maps_keeps_work_with_no_visible_pages() {
  printf '[]' > "$TMPROOT/a.json"
  assert_eq '{"49":[]}' "$(merge_page_maps 49 "$TMPROOT/a.json")" "пустая карта"
}

test_merge_page_maps_refuses_garbage() {
  local body
  for body in '{"message": "нет"}' 'не json' '[{"status": "x"}]' '[{"page_number": "1"}]'; do
    printf '%s' "$body" > "$TMPROOT/a.json"
    if ( merge_page_maps 49 "$TMPROOT/a.json" ) >/dev/null 2>&1; then
      fail "карта «$body» принята"
    fi
  done
}

# curl, отвечающий по адресу и печатающий код ответа (-w): 49, 50 и 60 —
# карты, 61 — 404 (служебная работа закрыта на боевом), 51 — 500.
stub_curl_by_url() {
  curl() {
    local out="" prev="" url=""
    for a in "$@"; do
      [[ "$prev" == -o ]] && out="$a"
      url="$a"
      prev="$a"
    done
    case "$url" in
      */works/49/page-map) printf '[{"page_number": 1}, {"page_number": 3}]' > "$out"; printf 200 ;;
      */works/50/page-map) printf '[{"page_number": 2}]' > "$out"; printf 200 ;;
      */works/60/page-map) printf '[{"page_number": 1}]' > "$out"; printf 200 ;;
      */works/61/page-map) printf '{"message": "Не найдено"}' > "$out"; printf 404 ;;
      *) printf '' > "$out"; printf 500 ;;
    esac
  }
}

test_fetch_work_pages_collects_every_work() {
  stub_curl_by_url
  printf '[{"id": 49, "title": "a"}, {"id": 50, "title": "b"}]' > "$TMPROOT/works.json"
  ( fetch_work_pages "$TMPROOT/works.json" "$TMPROOT/pages.json" ) || fail "сбор карт не прошёл"
  assert_eq '{"49":[1,3],"50":[2]}' "$(jq -c . "$TMPROOT/pages.json")" "файл -only-pages"
  [[ -z "$(ls "$TMPROOT"/pages.json.one* 2>/dev/null)" ]] || fail "остались временные файлы"
}

test_fetch_work_pages_refuses_failure_for_listed_work() {
  stub_curl_by_url
  printf '[{"id": 49, "title": "a"}, {"id": 51, "title": "c"}]' > "$TMPROOT/works.json"
  if ( fetch_work_pages "$TMPROOT/works.json" "$TMPROOT/pages.json" ) >/dev/null 2>&1; then
    fail "работа из списка без карты боевого принята — архив собрался бы со всеми её полосами"
  fi
  printf '[{"id": 61, "title": "d"}]' > "$TMPROOT/works.json"
  if ( fetch_work_pages "$TMPROOT/works.json" "$TMPROOT/pages.json" ) >/dev/null 2>&1; then
    fail "404 у работы каталога принят — список работ и карта разошлись"
  fi
}

test_fetch_work_pages_front_matter_404_is_left_out() {
  stub_curl_by_url
  printf '[{"id": 49, "title": "a"}]' > "$TMPROOT/works.json"
  printf '60\n61\n' > "$TMPROOT/kids"
  ( fetch_work_pages "$TMPROOT/works.json" "$TMPROOT/pages.json" "$TMPROOT/kids" ) >/dev/null 2>&1 || fail "404 служебной работы уронил выпуск"
  assert_eq '{"49":[1,3],"60":[1]}' "$(jq -c . "$TMPROOT/pages.json")" "карта без закрытой служебной"
}

test_fetch_work_pages_front_matter_other_failure_fails() {
  stub_curl_by_url
  printf '[{"id": 49, "title": "a"}]' > "$TMPROOT/works.json"
  printf '62\n' > "$TMPROOT/kids"
  if ( fetch_work_pages "$TMPROOT/works.json" "$TMPROOT/pages.json" "$TMPROOT/kids" ) >/dev/null 2>&1; then
    fail "500 у служебной работы принят за «закрыта»"
  fi
}

test_release_passes_only_pages_to_build_and_check() {
  local src
  src="$(sed -e ':a' -e 'N' -e '$!ba' -e 's/\\\n//g' "$TESTS_DIR/../static-release.sh")"
  grep -qE 'staticsite build .*-only-pages "\$ONLYPAGES"' <<<"$src" || fail "сборка не получает -only-pages"
  grep -q 'staticsite children "\$PROD"' <<<"$src" || fail "номера служебных работ не запрашиваются"
  grep -qE 'staticsite check .*-only-pages "\$ONLYPAGES"' <<<"$src" || fail "самопроверка не получает -only-pages"
}

run_tests
