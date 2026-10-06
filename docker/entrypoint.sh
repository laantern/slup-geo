#!/usr/bin/env bash
# Entrypoint апплаенса slup-geo:
#   1) стартует встроенный PostgreSQL (initdb при пустом volume);
#   2) при пустой БД или UPDATE_ON_START=true выполняет первый импорт/обновление;
#   3) запускает HTTP-сервис; периодичность обновлений задаёт UPDATE_SCHEDULE (внутри serve).
set -Eeo pipefail

export PGDATA="${PGDATA:-/data/pgdata}"
export DATA_DIR="${DATA_DIR:-/data}"
export HTTP_ADDR="${HTTP_ADDR:-:8080}"

export POSTGRES_DB="${POSTGRES_DB:-geo_db}"
export POSTGRES_USER="${POSTGRES_USER:-geo_user}"
export PGHOST="${PGHOST:-127.0.0.1}"
export PGPORT="${PGPORT:-5432}"
export PGDATABASE="${PGDATABASE:-$POSTGRES_DB}"
export PGUSER="${PGUSER:-$POSTGRES_USER}"

mkdir -p "$PGDATA" "$DATA_DIR/osm" "$DATA_DIR/tiles" "$DATA_DIR/state"

# Пароль БД: из ENV или сгенерированный при первом старте и сохранённый в volume.
# Наружу не публикуется — внутри контейнера им пользуются PostgreSQL и приложение.
db_password_file="$DATA_DIR/state/db_password"
if [ -n "${POSTGRES_PASSWORD:-}" ]; then
    db_password="$POSTGRES_PASSWORD"
elif [ -f "$db_password_file" ]; then
    db_password="$(cat "$db_password_file")"
else
    db_password="$(head -c 24 /dev/urandom | base64 | tr -d '\n/+=')"
    umask 077
    printf '%s' "$db_password" > "$db_password_file"
fi
export POSTGRES_PASSWORD="$db_password"
export PGPASSWORD="$db_password"

echo "[entrypoint] запуск PostgreSQL (PGDATA=$PGDATA)"
docker-entrypoint.sh postgres &
PG_PID=$!

SERVE_PID=""
shutdown() {
    echo "[entrypoint] остановка контейнера..."
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

# Первый импорт: пустая БД (нет схемы geo) или явный UPDATE_ON_START.
# Проверяем именно БД, а не файл-маркер: маркер без базы (или наоборот) не должен усыплять сервис.
schema_ready="false"
if psql -h "$PGHOST" -p "$PGPORT" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "SELECT to_regclass('geo.zones') IS NOT NULL" 2>/dev/null | grep -q '^t$'; then
    schema_ready="true"
fi

if [ "$schema_ready" != "true" ] || [ "${UPDATE_ON_START:-false}" = "true" ]; then
    echo "[entrypoint] первый импорт/обновление данных (PBF → PostGIS → представления → тайлы)"
    if ! slup-geo update; then
        echo "[entrypoint] ВНИМАНИЕ: update завершился с ошибкой; сервис стартует без свежих данных" >&2
    fi
fi

echo "[entrypoint] запуск slup-geo serve"
slup-geo serve &
SERVE_PID=$!

wait -n "$PG_PID" "$SERVE_PID"
echo "[entrypoint] один из процессов завершился — останавливаем контейнер"
