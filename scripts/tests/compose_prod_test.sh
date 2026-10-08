#!/usr/bin/env bash
# Боевой оверрайд compose в двух режимах хранилища. Нужен CLI docker compose,
# демон — нет: `compose config` только разбирает файлы. Раскладка повторяет
# боевую: --project-directory с .env внутри, а не --env-file — так compose
# читает и COMPOSE_PROFILES, на котором держится выбор режима.
set -Eeuo pipefail

TESTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=harness.sh
source "$TESTS_DIR/harness.sh"
ROOT="$(cd "$TESTS_DIR/../.." && pwd)"

if ! docker compose version >/dev/null 2>&1; then
  printf 'docker compose нет — пропускаю\n'
  exit 0
fi

# docker из snap не видит /tmp и скрытые каталоги (compose отвечает «no such
# file» или «permission denied»), поэтому каталог проектов ищем там, где его
# CLI файлы читает: пробуем $TMPDIR/по умолчанию, затем ~/compose-prod-test.
compose_can_read() {
  local d probe
  d="$(mktemp -d -p "$1")" || return 1
  printf 'services: {}\n' > "$d/c.yml"
  probe=0; docker compose -f "$d/c.yml" config >/dev/null 2>&1 || probe=1
  rm -rf "$d"
  return "$probe"
}
if ! compose_can_read "${TMPDIR:-/tmp}"; then
  mkdir -p "$HOME/compose-prod-test"
  export TMPDIR="$HOME/compose-prod-test"
fi

# prod_project <local|external> — каталог проекта с compose-файлами и .env.
prod_project() {
  cp "$ROOT/docker-compose.yml" "$ROOT/docker-compose.prod.yml" "$TMPROOT/"
  local ep pub
  if [[ "$1" == local ]]; then
    ep=http://seaweedfs:8333; pub=https://s3.lib.example.org
  else
    ep=https://s3.example.org; pub=https://s3.example.org
  fi
  cat > "$TMPROOT/.env" <<ENV
DB_PASSWORD=x
JWT_SECRET=x
ADMIN_EMAIL=a@example.org
ADMIN_PASSWORD=x
S3_ACCESS_KEY=k
S3_SECRET_KEY=s
S3_ENDPOINT=$ep
S3_PUBLIC_ENDPOINT=$pub
S3_REGION=default
DOMAIN=lib.example.org
CONFIG_DIR=$TMPROOT/config
ENV
  [[ "$1" == local ]] && printf 'COMPOSE_PROFILES=local-storage\n' >> "$TMPROOT/.env"
  [[ "$1" == external ]] && printf 'S3_CORS_ORIGINS=https://lib.example.org\n' >> "$TMPROOT/.env"
  return 0
}

prod() {
  docker compose --project-directory "$TMPROOT" \
    -f "$TMPROOT/docker-compose.yml" -f "$TMPROOT/docker-compose.prod.yml" "$@"
}

test_local_storage_keeps_seaweedfs() {
  prod_project local
  prod config --services | grep -qx seaweedfs ||
    fail "при COMPOSE_PROFILES=local-storage seaweedfs обязан быть в стеке: $(prod config --services | tr '\n' ' ')"
}

test_external_storage_drops_seaweedfs_and_its_dependencies() {
  prod_project external
  local services json
  services="$(prod config --services)" || { fail "compose config не разобрался"; return 0; }
  if grep -qx seaweedfs <<<"$services"; then
    fail "без профиля seaweedfs подниматься не должен: $services"
  fi
  json="$(prod config --format json)"
  [[ "$(jq -r '.services.backend.depends_on | keys | join(",")' <<<"$json")" == "migrate,postgres" ]] ||
    fail "backend обязан зависеть только от migrate и postgres: $(jq -c '.services.backend.depends_on' <<<"$json")"
  [[ "$(jq -r '.services.caddy.depends_on | keys | join(",")' <<<"$json")" == "frontend" ]] ||
    fail "caddy обязан зависеть только от frontend: $(jq -c '.services.caddy.depends_on' <<<"$json")"
}

test_backend_storage_comes_from_dotenv() {
  prod_project external
  local env
  env="$(prod config --format json | jq -c '.services.backend.environment')"
  [[ "$(jq -r .S3_ENDPOINT <<<"$env")" == https://s3.example.org ]] || fail "S3_ENDPOINT не из .env: $env"
  [[ "$(jq -r .S3_PUBLIC_ENDPOINT <<<"$env")" == https://s3.example.org ]] || fail "S3_PUBLIC_ENDPOINT не из .env: $env"
  [[ "$(jq -r .S3_REGION <<<"$env")" == default ]] || fail "S3_REGION не из .env: $env"
  [[ "$(jq -r .S3_CORS_ORIGINS <<<"$env")" == https://lib.example.org ]] || fail "S3_CORS_ORIGINS не из .env: $env"
}

# Каталог экземпляра монтируется во фронт и на боевом: без него nginx отдаёт
# иконки сборки, а /legal исчезает — читальня теряет своё лицо молча.
test_frontend_mounts_instance_dir() {
  prod_project external
  local json; json="$(prod config --format json)"
  jq -e --arg src "$TMPROOT/instance" \
    '.services.frontend.volumes | any(.source == $src and .target == "/usr/share/nginx/html/instance" and .read_only == true)' \
    <<<"$json" >/dev/null ||
    fail "frontend не монтирует ./instance в /usr/share/nginx/html/instance:ro: $(jq -c '.services.frontend.volumes' <<<"$json")"
}

# Переменные экземпляра (имя, описание, ссылки, подпись, ники) доходят до
# бэкенда из .env — список environment у сервиса явный, и пропущенная в нём
# переменная молча не существует внутри контейнера.
test_instance_vars_reach_backend() {
  local mode var json
  for mode in local external; do
    prod_project "$mode"
    cat >>"$TMPROOT/.env" <<'ENV'
SITE_NAME=Тестовая
SITE_DESCRIPTION=о собрании
SITE_SUPPORT_URL=https://example.org/give
SITE_CHANNEL_URL=https://example.org/news
SITE_AGE_RATING=18+
SITE_TAGLINE=собрания
RESERVED_NICKNAMES=a,b
ENV
    json="$(prod config --format json)"
    for var in SITE_NAME SITE_DESCRIPTION SITE_SUPPORT_URL SITE_CHANNEL_URL SITE_AGE_RATING SITE_TAGLINE RESERVED_NICKNAMES; do
      [[ -n "$(jq -r --arg v "$var" '.services.backend.environment[$v] // empty' <<<"$json")" ]] ||
        fail "$mode: $var не доходит до backend"
    done
  done
  # Локальный compose без прода — тот же путь.
  json="$(docker compose --project-directory "$TMPROOT" -f "$TMPROOT/docker-compose.yml" config --format json)"
  [[ "$(jq -r '.services.backend.environment.SITE_NAME // empty' <<<"$json")" == "Тестовая" ]] ||
    fail "локальный compose: SITE_NAME не доходит до backend"
}

# Имена образов: без переменных — те же, что Compose давал сам при имени
# проекта proofreader (на них держится release.sh); с IMAGE_PREFIX/IMAGE_TAG —
# готовые образы реестра, ничего не собирая.
test_image_names_default_and_registry() {
  prod_project external
  local json; json="$(prod config --format json)"
  local got
  got="$(jq -r '[.services.backend.image, .services.migrate.image, .services.frontend.image] | join(" ")' <<<"$json")"
  [[ "$got" == "proofreader-backend proofreader-migrate proofreader-frontend" ]] ||
    fail "имена по умолчанию: $got"
  printf 'IMAGE_PREFIX=ghcr.io/example/proofreader\nIMAGE_TAG=v1.2.0\n' >>"$TMPROOT/.env"
  json="$(prod config --format json)"
  got="$(jq -r '[.services.backend.image, .services.migrate.image, .services.frontend.image] | join(" ")' <<<"$json")"
  [[ "$got" == "ghcr.io/example/proofreader-backend:v1.2.0 ghcr.io/example/proofreader-migrate:v1.2.0 ghcr.io/example/proofreader-frontend:v1.2.0" ]] ||
    fail "имена из реестра: $got"
}

run_tests
