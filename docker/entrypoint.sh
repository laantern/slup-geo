#!/usr/bin/env bash
# Entrypoint апплаенса slup-geo:
#   1) стартует встроенный PostgreSQL (initdb при пустом volume), слушающий только loopback;
#   2) готовит роли БД: geo_owner — владелец данных (update), geo_reader — только SELECT (serve),
#      geo_user — администратор образа (приложению его пароль не выдаётся);
#   3) первый импорт выполняет синхронно; UPDATE_ON_START при готовой схеме — фоном, не останавливая serve;
#   4) запускает HTTP-сервис; периодичность обновлений задаёт UPDATE_SCHEDULE (внутри serve).
set -Eeo pipefail
umask 077

export PGDATA="${PGDATA:-/data/pgdata}"
export DATA_DIR="${DATA_DIR:-/data}"
export HTTP_ADDR="${HTTP_ADDR:-:8080}"
export APP_USER="${APP_USER:-slup}"

export POSTGRES_DB="${POSTGRES_DB:-geo_db}"
export POSTGRES_USER="${POSTGRES_USER:-geo_user}"
export PGHOST="${PGHOST:-127.0.0.1}"
export PGPORT="${PGPORT:-5432}"
export PGDATABASE="${PGDATABASE:-$POSTGRES_DB}"
export PGUSER="${PGUSER:-$POSTGRES_USER}"

# is_true понимает те же значения, что и приложение: 1/true/yes/on (регистр не важен).
is_true() {
    case "$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]')" in
        1|true|yes|on) return 0 ;;
        *) return 1 ;;
    esac
}

mkdir -p "$PGDATA" "$DATA_DIR/osm" "$DATA_DIR/tiles" "$DATA_DIR/state" "$DATA_DIR/tmp"
chown -R "$APP_USER" "$DATA_DIR/osm" "$DATA_DIR/tiles" "$DATA_DIR/state" "$DATA_DIR/tmp" 2>/dev/null || true

# Пароль geo_user: из ENV или сгенерированный при первом старте и сохранённый в volume.
# Файл остаётся root-only (0600): админ-пароль не должен быть доступен процессам приложения.
db_password_file="$DATA_DIR/state/db_password"
if [ -n "${POSTGRES_PASSWORD:-}" ]; then
    db_password="$POSTGRES_PASSWORD"
elif [ -f "$db_password_file" ]; then
    db_password="$(cat "$db_password_file")"
else
    db_password="$(head -c 24 /dev/urandom | base64 | tr -d '\n/+=')"
fi
printf '%s' "$db_password" > "$db_password_file"
chmod 600 "$db_password_file"
# chown -R по state выполняется раньше и мог вернуть файл slup'у на повторных стартах —
# админ-пароль обязан оставаться root-only (см. SECURITY.md).
chown 0:0 "$db_password_file" 2>/dev/null || true
export POSTGRES_PASSWORD="$db_password"
export PGPASSWORD="$db_password"

# Пароль geo_reader (serve ходит в БД только на чтение).
reader_password_file="$DATA_DIR/state/db_password_reader"
if [ -f "$reader_password_file" ]; then
    reader_password="$(cat "$reader_password_file")"
else
    reader_password="$(head -c 24 /dev/urandom | base64 | tr -d '\n/+=')"
fi
printf '%s' "$reader_password" > "$reader_password_file"
chmod 600 "$reader_password_file"

# Пароль geo_owner (update: владелец данных, без прав суперпользователя).
owner_password_file="$DATA_DIR/state/db_password_owner"
if [ -f "$owner_password_file" ]; then
    owner_password="$(cat "$owner_password_file")"
else
    owner_password="$(head -c 24 /dev/urandom | base64 | tr -d '\n/+=')"
fi
printf '%s' "$owner_password" > "$owner_password_file"
chmod 600 "$owner_password_file"
# Читаемые приложению — только пароли его ролей (для ручного docker exec).
chown "$APP_USER" "$reader_password_file" "$owner_password_file" 2>/dev/null || true

# Хвосты прерванных загрузок/сборок не должны мешать новому запуску.
rm -f "$DATA_DIR"/osm/*.part
rm -rf "${DATA_DIR:?}"/tmp/*

echo "[entrypoint] запуск PostgreSQL (PGDATA=$PGDATA, только 127.0.0.1)"
docker-entrypoint.sh postgres -c listen_addresses=127.0.0.1 &
PG_PID=$!

SERVE_PID=""
UPDATE_PID=""
stopping="false"
shutdown() {
    stopping="true"
    echo "[entrypoint] остановка контейнера..."
    if [ -n "$UPDATE_PID" ]; then
        kill -TERM "$UPDATE_PID" 2>/dev/null || true
    fi
    if [ -n "$SERVE_PID" ]; then
        kill -TERM "$SERVE_PID" 2>/dev/null || true
    fi
    kill -TERM "$PG_PID" 2>/dev/null || true
    wait || true
    exit 0
}
trap shutdown TERM INT

# Готовность только по TCP: временный сервер initdb слушает unix-сокет, а не 127.0.0.1.
ready="false"
for _ in $(seq 1 120); do
    if pg_isready -q -h "$PGHOST" -p "$PGPORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB"; then
        ready="true"
        break
    fi
    sleep 1
done
if [ "$ready" != "true" ]; then
    echo "[entrypoint] PostgreSQL не поднялся за 120 секунд" >&2
    exit 1
fi

# Административные операции — от суперпользователя (POSTGRES_USER) по TCP с паролем:
# работает при любой pg_hba, включая ужесточённую ниже.
admin_psql() {
    PGPASSWORD="$db_password" psql -q -v ON_ERROR_STOP=1 -h "$PGHOST" -p "$PGPORT" \
        -U "$POSTGRES_USER" -d "$POSTGRES_DB" "$@"
}

# Внешняя БД (DATABASE_URL): роли и права — забота владельца БД, апплаенс их не трогает.
if [ -z "${DATABASE_URL:-}" ]; then
    echo "[entrypoint] подготовка ролей и расширений БД"
    admin_psql <<SQL
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS hstore;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
-- geo_owner — владелец данных (update), geo_reader — только SELECT (serve).
-- geo_user (POSTGRES_USER) остаётся администратором и приложением не используется.
DO \$\$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'geo_owner') THEN
    CREATE ROLE geo_owner LOGIN PASSWORD '$owner_password' NOSUPERUSER;
  ELSE
    ALTER ROLE geo_owner WITH LOGIN PASSWORD '$owner_password' NOSUPERUSER;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'geo_reader') THEN
    CREATE ROLE geo_reader LOGIN PASSWORD '$reader_password' NOSUPERUSER;
  ELSE
    ALTER ROLE geo_reader WITH LOGIN PASSWORD '$reader_password' NOSUPERUSER;
  END IF;
END \$\$;
GRANT CREATE ON SCHEMA public TO geo_owner;
GRANT CREATE, TEMP ON DATABASE $POSTGRES_DB TO geo_owner;
CREATE SCHEMA IF NOT EXISTS geo AUTHORIZATION geo_owner;
ALTER SCHEMA geo OWNER TO geo_owner;
-- Данные прежних версий принадлежат geo_user — передаём владение geo_owner.
DO \$\$
DECLARE r record;
BEGIN
  FOR r IN SELECT c.relname, c.relkind FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
           WHERE n.nspname = 'geo' AND c.relkind IN ('r','m','v','p','S') LOOP
    EXECUTE format('ALTER %s geo.%I OWNER TO geo_owner',
      CASE r.relkind WHEN 'm' THEN 'MATERIALIZED VIEW' WHEN 'v' THEN 'VIEW' WHEN 'S' THEN 'SEQUENCE' ELSE 'TABLE' END, r.relname);
  END LOOP;
  FOR r IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
           WHERE n.nspname = 'public' AND c.relname LIKE 'planet_osm%' AND c.relkind = 'r' LOOP
    EXECUTE format('ALTER TABLE public.%I OWNER TO geo_owner', r.relname);
  END LOOP;
END \$\$;
GRANT CONNECT ON DATABASE $POSTGRES_DB TO geo_reader;
GRANT USAGE ON SCHEMA geo TO geo_reader;
GRANT SELECT ON ALL TABLES IN SCHEMA geo TO geo_reader;
ALTER DEFAULT PRIVILEGES FOR ROLE geo_owner IN SCHEMA geo GRANT SELECT ON TABLES TO geo_reader;
SQL

    # Ужесточаем pg_hba: loopback и локальный сокет — только scram. Иначе любой процесс
    # в контейнере (uid slup) заходит суперпользователем без пароля, и роль-модель не работает.
    # Заодно приводим postgresql.conf в соответствие с CLI-оверрайдом listen_addresses,
    # иначе reload логирует «parameter cannot be changed without restarting».
    conf="$PGDATA/postgresql.conf"
    if [ -f "$conf" ]; then
        sed -i -E "s|^[[:space:]]*#?[[:space:]]*listen_addresses[[:space:]]*=.*$|listen_addresses = '127.0.0.1'|" "$conf"
    fi
    hba="$PGDATA/pg_hba.conf"
    if [ -f "$hba" ]; then
        sed -i -E \
            -e 's|^(local[[:space:]]+all[[:space:]]+all[[:space:]]+).*$|\1scram-sha-256|' \
            -e 's|^(host[[:space:]]+all[[:space:]]+all[[:space:]]+127\.0\.0\.1/32[[:space:]]+).*$|\1scram-sha-256|' \
            -e 's|^(host[[:space:]]+all[[:space:]]+all[[:space:]]+::1/128[[:space:]]+).*$|\1scram-sha-256|' \
            "$hba"
        admin_psql -c "SELECT pg_reload_conf();" >/dev/null
        echo "[entrypoint] pg_hba: локальные подключения требуют пароль (scram)"
    fi
fi

# Команда и окружение обновления: geo_owner + его пароль, без админ-пароля в окружении.
# В режиме внешней БД убираем и PGPASSWORD (иначе пароль встроенной БД унаследуется детьми).
if [ -n "${DATABASE_URL:-}" ]; then
    update_env=(env -u POSTGRES_PASSWORD -u PGPASSWORD)
else
    update_env=(env -u POSTGRES_PASSWORD PGUSER=geo_owner PGPASSWORD="$owner_password")
fi
update_cmd=(gosu "$APP_USER" slup-geo update)

# Первый импорт: пустая БД (нет схемы geo).
# Провал — завершаемся с кодом 1: restart-политика повторит попытку, а не оставит 503 no_data навсегда.
schema_ready="false"
if psql -h "$PGHOST" -p "$PGPORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "SELECT to_regclass('geo.zones') IS NOT NULL" 2>/dev/null | grep -q '^t$'; then
    schema_ready="true"
fi

if [ "$schema_ready" != "true" ]; then
    echo "[entrypoint] первый импорт данных (PBF → PostGIS → представления → тайлы)"
    # Фоном, чтобы SIGTERM во время импорта доходил до update (trap знает UPDATE_PID).
    "${update_env[@]}" "${update_cmd[@]}" &
    UPDATE_PID=$!
    if ! wait "$UPDATE_PID"; then
        echo "[entrypoint] первый импорт не удался — контейнер завершается (restart-политика повторит)" >&2
        exit 1
    fi
    UPDATE_PID=""
    if [ "$stopping" = "true" ]; then
        exit 0
    fi
elif is_true "${UPDATE_ON_START:-false}"; then
    # Данные уже есть: обновление идёт фоном, serve стартует сразу и продолжает отдавать
    # прежние данные (advisory lock не даст пересечься с cron-обновлением).
    echo "[entrypoint] обновление при старте запущено фоном (сервис продолжает работать)"
    "${update_env[@]}" "${update_cmd[@]}" &
    UPDATE_PID=$!
fi

echo "[entrypoint] запуск slup-geo serve"
if [ -n "${DATABASE_URL:-}" ]; then
    env -u POSTGRES_PASSWORD -u PGPASSWORD gosu "$APP_USER" slup-geo serve &
else
    env -u POSTGRES_PASSWORD PGUSER=geo_reader PGPASSWORD="$reader_password" gosu "$APP_USER" slup-geo serve &
fi
SERVE_PID=$!

if wait -n "$PG_PID" "$SERVE_PID"; then
    exit_code=0
else
    exit_code=$?
fi
echo "[entrypoint] процесс завершился (код $exit_code) — останавливаем контейнер"
kill -TERM "$SERVE_PID" 2>/dev/null || true
kill -TERM "$PG_PID" 2>/dev/null || true
wait || true
exit "$exit_code"
