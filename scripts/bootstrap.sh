#!/usr/bin/env bash
# Разворачивание читальни на свежем сервере из снапшота backup.sh.
# Выполняется НА СЕРВЕРЕ, запускается scripts/deploy.sh под tmux.
#
#   bash bootstrap.sh --domain lib.example.org [--s3-domain s3.lib.example.org]
#                     [--acme-email you@example.org] --snapshot /opt/proofreader/snapshot
#                     [--admin-password <старый>] [--force-restore]
#                     [--external-s3 https://хост --s3-key K --s3-secret S [--s3-region R]]
#
# lib.sh отсюда НЕ источается намеренно: на фазах 1-3 клона репозитория ещё
# нет, а вместе с ним нет и lib.sh. Свои log/die ниже — их копия. Как только
# клон появляется, восстановление делает настоящий scripts/restore.sh
# подпроцессом, а не переписанная здесь параллельная реализация.
#
# Скрипт источается тестами — тогда main не запускается.
set -Eeuo pipefail

ROOT_DIR="${ROOT_DIR:-/opt/proofreader}"
APP_DIR="$ROOT_DIR/app"
CONFIG_DIR="${CONFIG_DIR:-$ROOT_DIR/config}"
STATE_DIR="$ROOT_DIR/state"
COMPOSE_MIN=2.24.4   # ровно с этой версии docker датирует поддержку `!reset` в
                      # compose-файле; 2.24.0-2.24.3 прошли бы эту проверку и
                      # упали бы позже невнятной ошибкой разбора YAML-тега

DOMAIN=""; S3_DOMAIN=""; ACME_EMAIL=""; SNAPSHOT_DIR=""
ADMIN_OLD_PASSWORD="admin"; FORCE_RESTORE=""
EXTERNAL_S3=""; S3_REGION_ARG="us-east-1"; S3_KEY_ARG=""; S3_SECRET_ARG=""

log() { printf '[%s] %s\n' "$(date -u +%H:%M:%S)" "$*" >&2; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

usage() {
  sed -n '2,10p' "${BASH_SOURCE[0]}" >&2
  exit "${1:-1}"
}

# ---------------------------------------------------------------------------
# Чистые функции: строки и файловая система, без docker и сети.
# ---------------------------------------------------------------------------

parse_args() {
  DOMAIN=""; S3_DOMAIN=""; ACME_EMAIL=""; SNAPSHOT_DIR=""
  ADMIN_OLD_PASSWORD="admin"; FORCE_RESTORE=""
  EXTERNAL_S3=""; S3_REGION_ARG="us-east-1"; S3_KEY_ARG=""; S3_SECRET_ARG=""
  while (( $# )); do
    case "$1" in
      --domain)         DOMAIN="${2:-}"; shift 2 ;;
      --s3-domain)      S3_DOMAIN="${2:-}"; shift 2 ;;
      --acme-email)     ACME_EMAIL="${2:-}"; shift 2 ;;
      --snapshot)       SNAPSHOT_DIR="${2:-}"; shift 2 ;;
      --admin-password) ADMIN_OLD_PASSWORD="${2:-}"; shift 2 ;;
      --force-restore)  FORCE_RESTORE=1; shift ;;
      --external-s3)    EXTERNAL_S3="${2:-}"; shift 2 ;;
      --s3-region)      S3_REGION_ARG="${2:-}"; shift 2 ;;
      --s3-key)         S3_KEY_ARG="${2:-}"; shift 2 ;;
      --s3-secret)      S3_SECRET_ARG="${2:-}"; shift 2 ;;
      -h | --help)      usage 0 ;;
      *) die "неизвестный аргумент: $1" ;;
    esac
  done
  [[ -n "$DOMAIN" ]]       || die "нужен --domain (например lib.example.org)"
  [[ -n "$SNAPSHOT_DIR" ]] || die "нужен --snapshot (каталог с приехавшим бэкапом)"
  # Схема или слэш означают, что передали URL: дальше это молча уехало бы в
  # Caddyfile и в подпись presigned-ссылок, где сломало бы и то и другое.
  [[ "$DOMAIN" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]] ||
    die "--domain должен быть доменом без схемы и пути, получено '$DOMAIN'"
  if [[ -n "$EXTERNAL_S3" ]]; then
    # Внешний S3: браузер ходит в хранилище прямо, поддомен s3.<домен> не нужен.
    [[ "$EXTERNAL_S3" =~ ^https://[a-z0-9.-]+(:[0-9]+)?$ ]] ||
      die "--external-s3 должен быть адресом вида https://хост, получено '$EXTERNAL_S3'"
    [[ -n "$S3_KEY_ARG" && -n "$S3_SECRET_ARG" ]] ||
      die "--external-s3 требует --s3-key и --s3-secret"
    [[ "$S3_KEY_ARG$S3_SECRET_ARG" =~ ^[A-Za-z0-9]+$ ]] ||
      die "ключи внешнего хранилища — только буквы и цифры: mc_run вшивает их в URL"
    [[ -z "$S3_DOMAIN" ]] ||
      die "--s3-domain при --external-s3 не нужен: браузер ходит прямо во внешнее хранилище"
  else
    : "${S3_DOMAIN:=s3.$DOMAIN}"
    [[ "$S3_DOMAIN" =~ ^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$ ]] ||
      die "--s3-domain должен быть доменом без схемы и пути, получено '$S3_DOMAIN'"
  fi
}

# gen_secret <байт> — hex, и только hex. Base64 сюда нельзя: ключи S3 уезжают
# в URL внутри mc_run (lib.sh), а пароль админа — в JSON логина, собранный
# вручную (export.sh). И то и другое ломается на спецсимволах.
gen_secret() { openssl rand -hex "$1"; }

# storage_env — строки .env про хранилище: внешний S3 (адрес и ключи из
# аргументов, CORS открыт домену читальни) или местный SeaweedFS (свежие
# ключи, профиль compose, поддомен для браузера).
storage_env() {
  if [[ -n "$EXTERNAL_S3" ]]; then
    cat <<ENV
S3_ENDPOINT=$EXTERNAL_S3
S3_PUBLIC_ENDPOINT=$EXTERNAL_S3
S3_REGION=$S3_REGION_ARG
S3_BUCKET=proofreader
S3_ACCESS_KEY=$S3_KEY_ARG
S3_SECRET_KEY=$S3_SECRET_ARG
S3_CORS_ORIGINS=https://$DOMAIN
ENV
  else
    cat <<ENV
S3_DOMAIN=$S3_DOMAIN
# SeaweedFS живёт на этом же сервере: сервис под профилем в docker-compose.prod.yml.
COMPOSE_PROFILES=local-storage
S3_ENDPOINT=http://seaweedfs:8333
S3_PUBLIC_ENDPOINT=https://$S3_DOMAIN
S3_REGION=us-east-1
S3_BUCKET=proofreader
S3_ACCESS_KEY=$(gen_secret 12)
S3_SECRET_KEY=$(gen_secret 24)
ENV
  fi
}

# write_env <путь> — создаёт .env при отсутствии. Существующий не трогает
# никогда: том Postgres инициализируется паролем один раз, при создании, и
# новый DB_PASSWORD на втором прогоне дал бы стек, не способный войти в свою базу.
write_env() {
  local path="$1"
  if [[ -f "$path" ]]; then
    log "$path уже есть — секреты беру из него, не перегенерирую"
    return 0
  fi
  umask 077
  cat > "$path" <<ENV
# Сгенерировано scripts/bootstrap.sh. Секреты НЕ перегенерируются при повторном
# прогоне: том Postgres инициализируется паролем при создании.
DOMAIN=$DOMAIN
CONFIG_DIR=$CONFIG_DIR
# export.sh ходит в API по этому адресу; порт бэкенда наружу не публикуется,
# и без этой строки на боевом он молча стучался бы в http://localhost:8080,
# где никого нет.
API_URL=https://$DOMAIN

SERVER_HOST=0.0.0.0
SERVER_PORT=8080
# Адрес, под которым читальню видит браузер — идёт на титульный лист каждой
# выгрузки (internal/api/download_source.go). Без этой строки бэкенд берёт
# дефолт http://localhost:3100 и на боевом клеймит им каждую скачанную книгу.
PUBLIC_BASE_URL=https://$DOMAIN
# Перед бэкендом на боевом стоит Caddy + nginx фронта, и X-Real-IP несёт
# настоящий адрес читателя (frontend/nginx.conf). Без этой строки предел
# частоты формы обратной связи считал бы адрес контейнера nginx.
TRUST_PROXY_HEADERS=true

DB_HOST=postgres
DB_PORT=5432
DB_USER=proofreader
DB_PASSWORD=$(gen_secret 24)
DB_NAME=proofreader
DB_SSLMODE=disable

JWT_SECRET=$(gen_secret 32)
JWT_EXPIRATION=24h
JWT_REFRESH_EXPIRATION=168h

ADMIN_EMAIL=admin@proofreader.local
ADMIN_PASSWORD=$(gen_secret 16)

$(storage_env)
S3_USE_PATH_STYLE=true
S3_PRESIGN_TTL=30m
ENV
  chmod 600 "$path"
  log "секреты сгенерированы: $path"
}

# render_s3_config <ключ> <секрет> — то же удостоверение, что в .env.
# Ключи обязаны совпадать, иначе хранилище не пустит ни бэкенд, ни mc.
render_s3_config() {
  cat <<JSON
{
  "identities": [
    {
      "name": "proofreader",
      "credentials": [
        {
          "accessKey": "$1",
          "secretKey": "$2"
        }
      ],
      "actions": ["Read", "Write", "List", "Tagging", "Admin"]
    }
  ]
}
JSON
}

# render_caddyfile <шаблон> <домен> <s3-домен> <почта|пусто> <есть-сертификат|пусто>
render_caddyfile() {
  local tmpl="$1" domain="$2" s3="$3" email="$4" own_cert="${5:-}"
  [[ -f "$tmpl" ]] || die "не найден шаблон Caddyfile: $tmpl"
  local email_line='/__ACME_EMAIL_LINE__/d'
  [[ -n "$email" ]] && email_line="s|__ACME_EMAIL_LINE__|\temail $email|"
  # Пятый параметр непустой — у основного домена свой сертификат (Origin
  # Certificate от Cloudflare), и ACME для него не нужен. Пусто — строка
  # уходит, работает Let's Encrypt.
  local tls_line='/__TLS_LINE__/d'
  [[ -n "$own_cert" ]] &&
    tls_line="s|__TLS_LINE__|\ttls /etc/caddy/certs/origin.pem /etc/caddy/certs/origin.key|"
  # Поддомена хранилища нет (внешний S3) — блок между маркерами уходит
  # целиком; есть — уходят одни маркеры.
  local s3_block='/^#S3-BLOCK-BEGIN$/d;/^#S3-BLOCK-END$/d'
  [[ -n "$s3" ]] || s3_block='/^#S3-BLOCK-BEGIN$/,/^#S3-BLOCK-END$/d'
  sed -e "$s3_block" -e "s|__DOMAIN__|$domain|g" -e "s|__S3_DOMAIN__|$s3|g" \
      -e "$email_line" -e "$tls_line" "$tmpl"
}

# phase_pending <каталог отметок> <имя> — 0, если фазу надо выполнять.
phase_pending() { [[ ! -e "$1/$2.done" ]]; }

# version_ge <есть> <надо> — сравнение по частям, а не по строке:
# "2.9.0" лексикографически больше "2.24.0", и это ровно та ошибка,
# из-за которой оверрайд с `!reset` упал бы уже после установки docker.
version_ge() {
  local have="$1" want="$2" h w i
  local -a ha wa
  IFS=. read -ra ha <<< "$have"
  IFS=. read -ra wa <<< "$want"
  for (( i = 0; i < ${#wa[@]}; i++ )); do
    h="${ha[i]:-0}"; w="${wa[i]:-0}"
    h="${h//[^0-9]/}"; w="${w//[^0-9]/}"
    (( 10#${h:-0} > 10#${w:-0} )) && return 0
    (( 10#${h:-0} < 10#${w:-0} )) && return 1
  done
  return 0
}

# ---------------------------------------------------------------------------
# Фазы. Каждая отмечается файлом в $STATE_DIR и при повторном прогоне пропускается.
# ---------------------------------------------------------------------------

run_phase() {
  local name="$1" fn="$2" force="${3:-}"
  # force нужен восстановлению: его отметку проверяет restore_guard, а пропуск
  # по отметке здесь сделал бы --force-restore мёртвым флагом.
  if [[ -z "$force" ]] && ! phase_pending "$STATE_DIR" "$name"; then
    log "фаза $name уже выполнена — пропускаю"
    return 0
  fi
  log "=== фаза $name ==="
  "$fn"
  touch "$STATE_DIR/$name.done"
}

phase_preflight() {
  [[ -d "$SNAPSHOT_DIR" ]] || die "каталог снапшота не найден: $SNAPSHOT_DIR"
  [[ -f "$SNAPSHOT_DIR/manifest.json" ]] || die "в снапшоте нет manifest.json: $SNAPSHOT_DIR"
  [[ -e "$SNAPSHOT_DIR/.INCOMPLETE" ]] && die "снапшот битый (.INCOMPLETE): $SNAPSHOT_DIR"
  [[ -n "$EXTERNAL_S3" || -d "$SNAPSHOT_DIR/files" ]] || die "в снапшоте нет files/: $SNAPSHOT_DIR"
  [[ -f "$SNAPSHOT_DIR/meta.tar" || -f "$SNAPSHOT_DIR/db.dump" ]] ||
    die "в снапшоте нет ни meta.tar, ни db.dump: $SNAPSHOT_DIR"

  # Место: снапшот уже лежит, столько же уйдёт в том хранилища, плюс образы.
  local need_kb free_kb snap_kb
  snap_kb="$(du -sk "$SNAPSHOT_DIR" | cut -f1)"
  need_kb=$(( snap_kb + 4 * 1024 * 1024 ))
  free_kb="$(df -Pk "$ROOT_DIR" | awk 'NR==2 {print $4}')"
  (( free_kb >= need_kb )) ||
    die "мало места: свободно $(( free_kb / 1024 )) МБ, нужно ~$(( need_kb / 1024 )) МБ (снапшот + том хранилища + образы)"

  # 80 и 443 обязаны быть свободны: их займёт Caddy, и без них не выпишется
  # сертификат. Чужой nginx на хосте — самая частая причина провала.
  local port
  for port in 80 443; do
    if ss -Hltn "sport = :$port" | grep -q .; then
      die "порт $port занят — освободи его (обычно это системный nginx/apache: systemctl disable --now nginx)"
    fi
  done
  log "предполёт пройден: снапшот цел, места хватает, 80 и 443 свободны"
}

phase_docker() {
  if command -v docker >/dev/null && docker compose version >/dev/null 2>&1; then
    log "docker уже стоит"
  else
    log "ставлю docker с get.docker.com"
    curl -fsSL https://get.docker.com -o /tmp/get-docker.sh || die "не скачался установщик docker"
    sh /tmp/get-docker.sh || die "установка docker не удалась"
    rm -f /tmp/get-docker.sh
  fi
  command -v docker >/dev/null || die "docker так и не появился в PATH"

  local have
  have="$(docker compose version --short 2>/dev/null || echo 0)"
  version_ge "$have" "$COMPOSE_MIN" ||
    die "нужен docker compose >= $COMPOSE_MIN (есть $have): боевой оверрайд снимает опубликованные порты через 'ports: !reset []', и на старом compose стек уехал бы в интернет всеми пятью портами"
  log "docker compose $have"

  # rsync и tmux ставит deploy.sh до передачи файлов; git нужен здесь, для
  # клона. unzip нужен export.sh (распаковывает архив /api/export) — без него
  # экспорт на боевом падает, хотя документация обещает, что он работает.
  # curl уже используется этой же фазой чуть выше (get.docker.com) — ставим
  # его и здесь, а не полагаемся на то, что он есть на образе по умолчанию.
  { command -v git >/dev/null && command -v unzip >/dev/null && command -v curl >/dev/null; } ||
    { apt-get update -qq && apt-get install -y -qq git unzip curl; } ||
    die "не удалось поставить git/unzip/curl"
}

# clone_from_bundle <бандл> <каталог> — клон из бандла; повторный вызов безобиден.
clone_from_bundle() {
  local bundle="$1" dest="$2"
  [[ -s "$bundle" ]] || die "бандл пуст или отсутствует: $bundle"
  if [[ -d "$dest/.git" ]]; then
    log "клон уже есть: $dest"
    return 0
  fi
  git clone -q "$bundle" "$dest" || die "не удалось клонировать из $bundle"
  log "репозиторий развёрнут в $dest ($(git -C "$dest" rev-parse --short HEAD))"
}

phase_clone() {
  local meta_work="$ROOT_DIR/meta"
  mkdir -p "$meta_work"
  if [[ -f "$SNAPSHOT_DIR/meta.tar" ]]; then
    tar -xf "$SNAPSHOT_DIR/meta.tar" -C "$meta_work" || die "не распаковался meta.tar"
  else
    cp "$SNAPSHOT_DIR/db.dump" "$meta_work/db.dump" || die "не скопировался db.dump"
  fi
  # Дамп несёт bcrypt-хеши паролей. umask тут бесполезен: под root tar
  # восстанавливает права из архива, а сам umask протёк бы дальше, на клон
  # всего дерева приложения. Поэтому явный chmod, и на каталог, и на файлы.
  chmod -R go-rwx "$meta_work"
  [[ -s "$meta_work/repo.bundle" ]] ||
    die "в снапшоте нет repo.bundle — код брать неоткуда. Сними свежий бэкап: ./scripts/backup.sh"
  clone_from_bundle "$meta_work/repo.bundle" "$APP_DIR"
}

# domain_guard <путь к .env> <домен> <поддомен хранилища> — die, если .env уже
# существует и настроен на другой домен ЛИБО другой поддомен хранилища.
# phase_config пропускается по отметке config.done при повторном прогоне,
# поэтому смена --domain или --s3-domain иначе тихо оставила бы старый
# конфиг: Caddy продолжал бы обслуживать прежнее имя (или старый
# S3_PUBLIC_ENDPOINT остался бы в .env), а приёмка стучалась бы в новый
# домен и падала без объяснения. Смена домена — осознанное действие, а не то,
# что должно происходить по умолчанию на повторном прогоне.
domain_guard() {
  local env_path="$1" domain="$2" s3_domain="$3" existing existing_s3
  [[ -f "$env_path" ]] || return 0
  # Смена хранилища на живом сервере — переезд по спеке
  # 2026-10-02-external-s3-storage, а не повторный bootstrap: отметки фаз
  # пропустили бы конфиг, и .env остался бы от прежнего режима.
  local existing_ep
  existing_ep="$(grep -oP '^S3_ENDPOINT=\K.*' "$env_path" || true)"
  if [[ -n "$existing_ep" ]]; then
    local was="local" want="local"
    [[ "$existing_ep" == "http://seaweedfs:8333" ]] || was="external"
    [[ -z "$EXTERNAL_S3" ]] || want="external"
    [[ "$was" == "$want" ]] ||
      die "$env_path записан для хранилища $existing_ep, а прогон просит $([[ $want == external ]] && echo "$EXTERNAL_S3" || echo 'местный SeaweedFS'). Смена хранилища на живом сервере идёт по спеке 2026-10-02-external-s3-storage, а не повторным bootstrap"
  fi
  existing="$(grep -oP '^DOMAIN=\K.*' "$env_path" || true)"
  existing_s3="$(grep -oP '^S3_DOMAIN=\K.*' "$env_path" || true)"
  [[ -z "$existing" || "$existing" == "$domain" ]] ||
    die "$env_path уже настроен на домен $existing, а передан $domain. Чтобы сменить домен, осознанно удали отметку фазы: rm $STATE_DIR/config.done — и учти, что новый домен обычно требует нового восстановления (--force-restore), если снапшот тоже другой"
  # Во внешнем режиме поддомена хранилища нет, а живой .env боевого, записанный
  # прежним bootstrap, ещё несёт S3_DOMAIN SeaweedFS: сравнивать его не с чем,
  # и отказ «другой поддомен» уводил бы оператора не туда. Смену режима выше
  # уже поймала проверка S3_ENDPOINT.
  [[ -n "$EXTERNAL_S3" || -z "$existing_s3" || "$existing_s3" == "$s3_domain" ]] ||
    die "$env_path уже настроен на поддомен хранилища $existing_s3, а передан $s3_domain. Чтобы сменить его, осознанно удали отметку фазы: rm $STATE_DIR/config.done — и учти, что новый домен обычно требует нового восстановления (--force-restore), если снапшот тоже другой"
}

phase_config() {
  # Снапшот, снятый до поддержки внешнего хранилища, несёт compose, которому
  # нужен S3_DOMAIN, и restore.sh без --db-only. Без этой проверки внешний
  # прогон на нём падал поздно — на сборке образов в phase_infra — и словами,
  # из которых причины не понять. Признак — --db-only в restore.sh клона:
  # он появился той же веткой, что и compose без обязательного S3_DOMAIN.
  if [[ -n "$EXTERNAL_S3" ]] && ! grep -q -- '--db-only' "$APP_DIR/scripts/restore.sh" 2>/dev/null; then
    die "снапшот снят до поддержки внешнего хранилища (в его restore.sh нет --db-only) — сними свежий backup.sh с кодом этой ветки"
  fi
  mkdir -p "$CONFIG_DIR"
  chmod 700 "$CONFIG_DIR"
  write_env "$APP_DIR/.env"

  umask 077
  if [[ -z "$EXTERNAL_S3" ]]; then
    # Ключи для хранилища берём из .env — они обязаны совпасть, иначе ни бэкенд,
    # ни mc внутри restore.sh не получат доступа к бакету.
    local key secret
    key="$(grep -oP '^S3_ACCESS_KEY=\K.*' "$APP_DIR/.env")"
    secret="$(grep -oP '^S3_SECRET_KEY=\K.*' "$APP_DIR/.env")"
    [[ -n "$key" && -n "$secret" ]] || die "в $APP_DIR/.env нет ключей S3"
    render_s3_config "$key" "$secret" > "$CONFIG_DIR/s3_config.json"
    # 644, а не 600: файл монтируется внутрь SeaweedFS, и при 600 контейнер
    # уходит в рестарт-петлю с «fail to load config file: permission denied» —
    # проверено на боевом. Ключи наружу не открываются: $CONFIG_DIR и
    # /opt/proofreader — 700 root, а bind-mount проверяет права самого файла.
    chmod 644 "$CONFIG_DIR/s3_config.json"
  fi
  local own_cert=""
  [[ -f "$CONFIG_DIR/caddy-certs/origin.pem" && -f "$CONFIG_DIR/caddy-certs/origin.key" ]] &&
    own_cert=yes
  render_caddyfile "$APP_DIR/docker/caddy/Caddyfile.tmpl" \
    "$DOMAIN" "$S3_DOMAIN" "$ACME_EMAIL" "$own_cert" > "$CONFIG_DIR/Caddyfile"
  chmod 644 "$CONFIG_DIR/Caddyfile"
  log "конфиги готовы: $CONFIG_DIR"
}

# compose <args...> — боевой стек всегда поднимается двумя файлами.
compose() {
  docker compose --project-directory "$APP_DIR" \
    -f "$APP_DIR/docker-compose.yml" -f "$APP_DIR/docker-compose.prod.yml" "$@"
}

# wait_ready <описание> <контейнер> <попыток> <команда...> — тем же приёмом,
# что и verify-restore.sh: при провале показываем хвост лога, а не голое «не поднялся».
wait_ready() {
  local desc="$1" container="$2" tries="$3"; shift 3
  local i
  for (( i = 1; i <= tries; i++ )); do
    if "$@" >/dev/null 2>&1; then log "$desc готов"; return 0; fi
    sleep 2
  done
  log "последние строки лога $container:"
  docker logs --tail 30 "$container" >&2 || true
  die "$desc не поднялся за $(( tries * 2 ))с"
}

phase_infra() {
  log "собираю образы (это самая долгая часть, 10-20 минут на свежей машине)"
  compose build || die "сборка образов не удалась"
  if [[ -n "$EXTERNAL_S3" ]]; then
    compose up -d postgres || die "инфраструктура не поднялась"
    wait_ready postgres proofreader-postgres 30 docker exec proofreader-postgres pg_isready -U proofreader
    log "хранилище внешнее ($EXTERNAL_S3): SeaweedFS не поднимаю, бакеты и CORS бэкенд заведёт сам на старте"
    return 0
  fi
  # Только инфраструктура: бэкенд поднимется ПОСЛЕ восстановления. Живые
  # соединения приложения превратили бы pg_restore --clean в ожидание блокировок.
  compose up -d postgres seaweedfs || die "инфраструктура не поднялась"
  wait_ready postgres  proofreader-postgres  30 docker exec proofreader-postgres pg_isready -U proofreader
  wait_ready seaweedfs proofreader-seaweedfs 30 docker exec proofreader-seaweedfs wget -q -O /dev/null http://127.0.0.1:8333/healthz

  # Бакета в свежем хранилище нет, а mirror в restore.sh его не создаёт.
  ( cd "$APP_DIR" && source scripts/lib.sh && load_config &&
    mc_run mb --ignore-existing "seaweed/$S3_BUCKET" ) || die "не создался бакет"
}

# restore_guard <каталог отметок> — 0, если восстановление надо выполнять.
# Повторный прогон почти всегда означает «продолжить с упавшей фазы» (сертификат,
# приёмка), а не «восстановить заново»: отказ здесь заставлял бы гонять полное
# восстановление ради ретрая последней фазы. Но и молчать нельзя — прогон с
# другим снапшотом тихо оставил бы на сервере старые данные.
restore_guard() {
  local state="$1"
  if [[ -e "$state/restore.done" && -z "$FORCE_RESTORE" ]]; then
    log "ВНИМАНИЕ: восстановление уже выполнялось и пропущено — данные на сервере остаются от прошлого прогона. Чтобы восстановить заново, в том числе из другого снапшота, перезапусти с --force-restore"
    return 1
  fi
  return 0
}

# invalidate_post_restore_marks <каталог отметок> — снять отметки фаз, чей
# результат восстановление откатывает.
#
# Восстановление перезаписывает базу дампом целиком, включая таблицу users.
# Значит смена пароля администратора, выполненная прошлым прогоном, исчезает —
# на сервере снова стоит пароль из бэкапа. Отметка admin.done при этом лежит с
# того прогона, run_phase пропускает фазу по ней, и публичный сервер остаётся
# с паролем из бэкапа: ровно то, чего phase_admin существует не допустить.
# Приёмка (smoke) по той же причине недействительна — она проверяла прежние
# данные, а прогон без неё отрапортовал бы BOOTSTRAP DONE, не проверив
# восстановленные.
invalidate_post_restore_marks() {
  local state="$1"
  rm -f "$state/admin.done" "$state/smoke.done"
}

phase_restore() {
  # restore.sh распаковывает второй экземпляр дампа через mktemp -d — по
  # умолчанию это /tmp, а на Ubuntu /tmp обычно tmpfs размером в половину
  # ОЗУ. phase_preflight мерил свободное место только на файловой системе
  # $ROOT_DIR, поэтому временный каталог для распаковки обязан быть там же.
  local tmp_dir="$ROOT_DIR/tmp"
  mkdir -p "$tmp_dir"
  # Настоящий restore.sh из клона, а не переписанная здесь копия: ключи S3,
  # имена контейнеров и сеть он возьмёт из .env через load_config.
  local args=("$SNAPSHOT_DIR" --yes)
  # Внешний S3: сканы уже там, снапшот приехал без files/ — только база.
  [[ -z "$EXTERNAL_S3" ]] || args+=(--db-only)
  ( cd "$APP_DIR" && TMPDIR="$tmp_dir" ./scripts/restore.sh "${args[@]}" ) ||
    die "восстановление не удалось"
}

phase_app() {
  # migrate отработает на восстановленной базе вхолостую: код из бандла — тот
  # самый, что делал дамп, версия схемы в дампе совпадает. Явный список без
  # caddy: сайт не должен становиться публично доступным раньше, чем сменится
  # пароль администратора (phase_admin) — caddy поднимает phase_smoke, уже
  # после смены.
  if [[ -n "$EXTERNAL_S3" ]]; then
    compose up -d postgres migrate backend frontend || die "приложение не поднялось"
  else
    compose up -d postgres seaweedfs migrate backend frontend || die "приложение не поднялось"
  fi
  wait_ready "бэкенд" proofreader-backend 60 \
    docker run --rm --network proofreader_default curlimages/curl -sS -f http://backend:8080/api/works
  log "стек поднят (без caddy — наружу открываемся после смены пароля администратора)"
}

# curl_in <args...> — запрос изнутри сети стека. Порты наружу не публикуются,
# и до выписки сертификатов публичный адрес ещё не работает.
curl_in() { docker run --rm --network proofreader_default curlimages/curl -sS "$@"; }

# unescape_json_url <строка> — Go-шный json.NewEncoder экранирует & как \u0026,
# а presigned-ссылка склеена из параметров амперсандами. Без распаковки
# скачивание превью падает, и виноватой выглядит подпись, а не кодировка.
unescape_json_url() { printf '%s' "$1" | sed 's/\\u0026/\&/g'; }

# admin_login <адрес> <пароль> — печатает тело ответа логина или ничего.
# Тело, а не только токен: там же лежит id администратора, и это единственный
# способ его узнать. Ходить за ним в /api/auth/me нельзя — эндпоинт отвечает
# 401 на любой валидный токен (ключ контекста кладётся типизированным, а
# читается нетипизированной строкой), см. internal/api/auth_handler.go.
admin_login() {
  curl_in -X POST http://backend:8080/api/auth/login \
    -H 'Content-Type: application/json' \
    -d "{\"email\":\"$1\",\"password\":\"$2\"}" || true
}

# admin_field <тело> <token|id> — вытащить поле из ответа логина.
# id идёт первым полем внутри "user", раньше "token", поэтому head -1 берёт
# именно администратора.
admin_field() {
  case "$2" in
    token) grep -oP '"token":\s*"\K[^"]+' <<< "$1" | head -1 || true ;;
    id)    grep -oP '"id":\s*\K[0-9]+'    <<< "$1" | head -1 || true ;;
  esac
}

phase_admin() {
  local email old new id token body
  email="$(grep -oP '^ADMIN_EMAIL=\K.*' "$APP_DIR/.env")"
  new="$(grep -oP '^ADMIN_PASSWORD=\K.*' "$APP_DIR/.env")"
  old="$ADMIN_OLD_PASSWORD"

  # Пробуем НОВЫМ паролем первым. Если прошлый прогон сменил пароль на
  # сервере, но упал до записи CREDENTIALS (или в дампе не было админа с этим
  # адресом, и seedAdminUser создал его сразу с ADMIN_PASSWORD из .env),
  # старого пароля больше нигде не существует — вход старым гарантированно
  # провалится, и путь «почини причину и запусти снова» перестанет работать.
  body="$(admin_login "$email" "$new")"
  token="$(admin_field "$body" token)"
  if [[ -n "$token" ]]; then
    log "администратор уже входит новым паролем — смену пропускаю"
  else
    # Пользователи приехали из дампа: seedAdminUser создаёт админа только если
    # его нет, поэтому ADMIN_PASSWORD из .env на восстановленную базу не подействовал.
    body="$(admin_login "$email" "$old")"
    token="$(admin_field "$body" token)"
    [[ -n "$token" ]] ||
      die "не вошёл админом ($email) ни старым, ни новым паролем. На боевом остался пароль из бэкапа — это публичный сервер, так оставлять нельзя. Передай верный: --admin-password '<пароль>'"

    id="$(admin_field "$body" id)"
    [[ -n "$id" ]] || die "в ответе логина нет id администратора: $body"

    curl_in -f -X PUT "http://backend:8080/api/users/$id" \
      -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
      -d "{\"password\":\"$new\"}" >/dev/null ||
      die "не удалось сменить пароль администратора"

    token="$(admin_login "$email" "$new")"
    [[ -n "$token" ]] || die "новый пароль не работает — вход не удался"
  fi

  umask 077
  cat > "$CONFIG_DIR/CREDENTIALS" <<CRED
Читальня:      https://$DOMAIN
Хранилище:     ${EXTERNAL_S3:-https://$S3_DOMAIN}
Администратор: $email
Пароль:        $new

Остальные учётные записи приехали из бэкапа со своими прежними паролями —
смена коснулась только администратора.
CRED
  chmod 600 "$CONFIG_DIR/CREDENTIALS"
  log "реквизиты администратора в $CONFIG_DIR/CREDENTIALS"
}

phase_smoke() {
  # Caddy поднимается только здесь, а не в phase_app: он единственный, кто
  # слушает 80/443, и до этой строки пароль администратора уже сменён
  # (phase_admin отработала раньше). Поднять его вместе с остальным стеком
  # значило бы отдать сайт в интернет с паролем из бэкапа на неопределённое
  # время — если phase_admin упадёт (например, --admin-password не подошёл),
  # окно остаётся открытым до следующего успешного прогона.
  compose up -d caddy || die "caddy не поднялся"

  # Всё снаружи, через домен: только так проверяется и сертификат, и подпись.
  local i work page url ctype
  for (( i = 1; i <= 60; i++ )); do
    curl -fsS -o /dev/null "https://$DOMAIN/" && break
    (( i == 60 )) && die "https://$DOMAIN/ не отвечает. Обычно это A-запись не на этот сервер или занятый 80-й порт: docker logs proofreader-caddy"
    sleep 5
  done
  log "главная отдаёт 200"

  if [[ -z "$EXTERNAL_S3" ]]; then
    curl -fsS -o /dev/null "https://$S3_DOMAIN/" ||
      log "ВНИМАНИЕ: https://$S3_DOMAIN/ не ответил кодом 2xx — для корня бакета это нормально, важна проверка превью ниже"
  fi

  # head -1 берёт первое совпадение "id": в потоке — безопасно, пока id
  # остаётся первым полем объекта работы в JSON-ответе; поменяется порядок
  # полей — можно выхватить чужой id (например, из вложенной структуры).
  work="$(curl -fsS "https://$DOMAIN/api/works" | grep -oP '"id":\s*\K[0-9]+' | head -1)" || true
  [[ -n "$work" ]] || die "/api/works не отдал ни одной работы — восстановление прошло вхолостую?"
  page="$(curl -fsS "https://$DOMAIN/api/works/$work/page-map" |
    grep -oP '"page_number":\s*\K[0-9]+' | head -1)" || true
  [[ -n "$page" ]] || die "у работы $work нет страниц"
  url="$(curl -fsS "https://$DOMAIN/api/works/$work/pages/by-number/$page" |
    grep -oP '"preview_url":\s*"\K[^"]+')" || true
  [[ -n "$url" ]] || die "у страницы $page работы $work нет preview_url"
  url="$(unescape_json_url "$url")"

  ctype="$(curl -fsS -o /tmp/smoke.png -w '%{content_type}' "$url")" ||
    die "превью не скачалось. $(if [[ -n "$EXTERNAL_S3" ]]; then echo "Убедись, что S3_PUBLIC_ENDPOINT в .env — $EXTERNAL_S3 и что бакеты на месте"; else echo "Это проверка подписи SigV4 через $S3_DOMAIN: убедись, что S3_PUBLIC_ENDPOINT в .env — именно https://$S3_DOMAIN, и что Caddy не переписывает Host"; fi)"
  [[ "$ctype" == image/png ]] || die "превью пришло не картинкой, а '$ctype'"
  [[ -s /tmp/smoke.png ]] || die "превью пустое"
  rm -f /tmp/smoke.png
  log "превью скачано через ${EXTERNAL_S3:-$S3_DOMAIN}: подпись сходится"
}

main() {
  # Первая строка функции, до parse_args и любых проверок: ловушка обязана
  # стоять раньше самого раннего возможного die(). Иначе отказ из parse_args,
  # из проверки root или из domain_guard не печатает "BOOTSTRAP FAILED" —
  # tail -f живого файла лога при этом не заканчивается сам, и deploy.sh
  # висит вечно, ожидая маркер, который никто не напишет (именно так и
  # обнаружилось на domain_guard — ради которой эту ловушку сюда и добавляли).
  # Симметрично маркеру успеха (BOOTSTRAP DONE): deploy.sh ловит провал по
  # отдельному маркеру, а не по подстроке "ERROR:" — она встречается и в
  # выводе вложенных инструментов (например, pg_restore) без провала всего
  # прогона. Ловушка на EXIT, а не на ERR: почти все отказы здесь идут через
  # die(), а die() завершает скрипт explicit exit'ом — ERR на explicit exit
  # не срабатывает (проверено отдельно), поэтому единственный надёжный
  # способ поймать «прогон закончился неуспехом» — проверить код выхода в EXIT.
  trap '[[ $? -eq 0 ]] || log "BOOTSTRAP FAILED"' EXIT

  parse_args "$@"
  # Проверка здесь, а не в phase_preflight: mkdir ниже выполняется раньше любой
  # фазы, а сама фаза при повторном прогоне пропускается по отметке в state/.
  [[ "$(id -u)" == "0" ]] || die "нужен root: ставим пакеты, пишем в $ROOT_DIR и слушаем 80/443"
  mkdir -p "$ROOT_DIR" "$STATE_DIR"
  # $APP_DIR/.env переживает между прогонами (клон и конфиги на диске, а не
  # только отметки в state/) — сверяем домен и поддомен хранилища до старта
  # любой фазы.
  domain_guard "$APP_DIR/.env" "$DOMAIN" "$S3_DOMAIN"

  log "разворачивание: домен $DOMAIN, хранилище ${EXTERNAL_S3:-$S3_DOMAIN}, снапшот $SNAPSHOT_DIR"
  run_phase preflight phase_preflight
  run_phase docker    phase_docker
  run_phase clone     phase_clone
  run_phase config    phase_config
  run_phase infra     phase_infra
  # Восстановление намеренно не идемпотентно, а повторный прогон в основном
  # значит «продолжить с упавшей фазы» — поэтому вместо отказа restore_guard
  # печатает предупреждение и пропускает фазу; --force-restore восстанавливает
  # заново, в том числе из другого снапшота.
  local restore_skipped=""
  if restore_guard "$STATE_DIR"; then
    run_phase restore phase_restore "$FORCE_RESTORE"
    invalidate_post_restore_marks "$STATE_DIR"
  else
    restore_skipped=1
  fi
  run_phase app       phase_app
  run_phase admin     phase_admin
  run_phase smoke     phase_smoke
  # Предупреждение restore_guard ушло в начало лога, за сотни строк до этого
  # места, — повторяем перед финальным маркером, чтобы оператор его не пролистал.
  [[ -z "$restore_skipped" ]] ||
    log "ВНИМАНИЕ: восстановление было пропущено — данные на сервере остаются от прошлого прогона. Для восстановления заново, в том числе из другого снапшота: --force-restore"
  log "готово: https://$DOMAIN — реквизиты в $CONFIG_DIR/CREDENTIALS"
  log "BOOTSTRAP DONE"
}

# Источается тестами — тогда main не запускается.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  main "$@"
fi
