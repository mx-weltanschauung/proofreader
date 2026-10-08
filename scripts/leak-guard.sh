#!/usr/bin/env bash
# Сторож утечек: проходит отслеживаемые файлы репозитория и печатает строки,
# которым нечего делать в открытом коде, — `путь:строка: правило`.
#
#   scripts/leak-guard.sh [корень]        # по умолчанию — корень репозитория
#   LEAK_DENYLIST=файл scripts/leak-guard.sh
#
# Ловит сам: строки вида ключей и токенов, публичные IPv4, адреса почты на
# общих почтовых доменах. LEAK_DENYLIST — файл подстрок конкретного
# экземпляра (домен, адрес сервера, имя провайдера, личная почта), по одной на
# строку; без переменной проверяются только общие шаблоны.
#
# Код: 0 — чисто, 1 — есть находки, 2 — ошибка запуска.
set -Eeuo pipefail

ROOT="${1:-$(git rev-parse --show-toplevel)}"

if [[ -n "${LEAK_DENYLIST:-}" && ! -r "$LEAK_DENYLIST" ]]; then
  echo "leak-guard: LEAK_DENYLIST=$LEAK_DENYLIST не читается" >&2
  exit 2
fi

# Отслеживаемые текстовые файлы, кроме vendor/ (чужой код со своими
# примерами ключей) и самого сторожа с его тестом (в них шаблоны).
files() {
  git -C "$ROOT" ls-files -z -- . ':!vendor/' ':!scripts/leak-guard.sh' ':!scripts/tests/leak_guard_test.sh'
}

PATTERNS=(
  'AKIA[0-9A-Z]{16}'
  'sk-ant-[A-Za-z0-9_-]{10,}'
  'gh[pousr]_[A-Za-z0-9]{20,}'
  'y0_[A-Za-z0-9_-]{20,}'
  'AQAAAA[A-Za-z0-9_-]{20,}'
  'BEGIN [A-Z ]*PRIVATE KEY'
  '[A-Za-z0-9._%+-]+@(gmail|yandex|ya|mail|bk|inbox|list|rambler|outlook|hotmail|icloud|proton|protonmail)\.(com|ru|me)'
)

found=0
report() { found=1; printf '%s\n' "$1"; }

cd "$ROOT"

for p in "${PATTERNS[@]}"; do
  while IFS= read -r line; do report "$line: шаблон $p"; done < <(
    files | xargs -0 -r grep -nIE -- "$p" 2>/dev/null || true)
done

# IPv4 — все, кроме частных, петли, «любого» и документационных диапазонов.
# Номер версии из пяти частей (1.2.3.4.5) адресом не считается, сеть в
# записи CIDR (173.245.48.0/20 — диапазоны CDN в конфигах) — тоже: это
# чужая опубликованная сеть, а не адрес нашего сервера.
while IFS= read -r line; do
  ips="$(grep -oE '(^|[^0-9.])([0-9]{1,3}\.){3}[0-9]{1,3}([^0-9./]|$)' <<<"${line#*:*:}" |
    grep -oE '([0-9]{1,3}\.){3}[0-9]{1,3}' || true)"
  for ip in $ips; do
    IFS=. read -r a b c _ <<<"$ip"
    a=$((10#$a)) b=$((10#$b)) c=$((10#$c))  # 090 — не восьмеричное
    (( a > 255 || b > 255 || c > 255 )) && continue
    case "$a" in 0|10|127) continue ;; esac
    (( a == 172 && b >= 16 && b <= 31 )) && continue
    (( a == 192 && b == 168 )) && continue
    (( a == 169 && b == 254 )) && continue
    [[ "$a.$b.$c" == 192.0.2 || "$a.$b.$c" == 198.51.100 || "$a.$b.$c" == 203.0.113 ]] && continue
    report "${line%%:*}:$(cut -d: -f2 <<<"$line"): публичный IPv4 $ip"
  done
done < <(files | xargs -0 -r grep -nIE -- '([0-9]{1,3}\.){3}[0-9]{1,3}' 2>/dev/null || true)

if [[ -n "${LEAK_DENYLIST:-}" ]]; then
  while IFS= read -r word; do
    [[ -z "$word" ]] && continue
    while IFS= read -r line; do report "$line: слово экземпляра"; done < <(
      files | xargs -0 -r grep -nIiF -- "$word" 2>/dev/null || true)
  done <"$LEAK_DENYLIST"
fi

exit "$found"
