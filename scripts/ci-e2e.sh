#!/usr/bin/env bash
# Сквозная проверка собранного образа на маленьком PBF (Монако): первый импорт, API/тайлы,
# обновление по расписанию (UPDATE_SCHEDULE работает правами update, а не serve) и наличие
# лицензий в образе.
# Использование: ./scripts/ci-e2e.sh <образ> [PBF_URL]
set -euo pipefail

IMAGE="${1:?укажите образ, например slup-geo:ci}"
PBF_URL="${2:-https://download.geofabrik.de/europe/monaco-latest.osm.pbf}"
NAME="slup-geo-e2e-$$"
PORT="${E2E_PORT:-18080}"
BASE="http://127.0.0.1:$PORT"

cleanup() {
    docker logs "$NAME" > "${E2E_LOG:-/tmp/slup-geo-e2e.log}" 2>&1 || true
    docker rm -f -v "$NAME" >/dev/null 2>&1 || true
}
trap cleanup EXIT

# fetch читает ответ целиком: «curl | grep -q» под pipefail ложно падает, когда grep
# закрывает поток раньше curl (SIGPIPE, код 23).
fetch() {
    curl -fsS "$@" 2>/dev/null || true
}

fail() {
    echo "ОШИБКА: $*" >&2
    docker logs --tail 80 "$NAME" >&2 || true
    exit 1
}

# Лицензии и уведомления лежат в образе (бинарная поставка MIT/BSD/Apache/GPL-компонентов).
docker run --rm --entrypoint sh "$IMAGE" -c '
    set -e
    for f in /usr/share/doc/slup-geo/LICENSE /usr/share/doc/slup-geo/THIRD_PARTY_LICENSES.md \
             /usr/share/doc/slup-geo/debian-packages.tsv /usr/share/doc/slup-geo/go-licenses/golang-stdlib-LICENSE \
             /usr/share/doc/tilemaker/THIRD_PARTY_NOTICES.txt /usr/share/doc/tilemaker/LICENCE.txt \
             /usr/local/share/slup-geo/vendor/LICENSE-maplibre-gl.txt /usr/local/share/slup-geo/vendor/LICENSE-pmtiles.txt; do
        test -s "$f" || { echo "нет $f" >&2; exit 1; }
    done
    ls /usr/share/doc/slup-geo/go-licenses/github.com/jackc/ | grep -q pgx
' || fail "в образе нет файлов лицензий"
echo "OK: лицензии в образе"

docker run -d --name "$NAME" \
    -e PBF_URL="$PBF_URL" \
    -e PBF_PATH=/data/osm/e2e.osm.pbf \
    -e UPDATE_SCHEDULE="* * * * *" \
    -p "127.0.0.1:$PORT:8080" \
    "$IMAGE" >/dev/null

# Первый импорт: /health → ok.
for _ in $(seq 1 120); do
    if grep -q '"status":"ok"' <<<"$(fetch "$BASE/health")"; then
        break
    fi
    docker ps -q -f "name=$NAME" | grep -q . || fail "контейнер завершился во время первого импорта"
    sleep 5
done
grep -q '"status":"ok"' <<<"$(fetch "$BASE/health")" || fail "сервис не стал готов за 10 минут"
echo "OK: первый импорт"

first_import="$(sed -n 's/.*"importedAt":"\([^"]*\)".*/\1/p' <<<"$(fetch "$BASE/status")")"
[ -n "$first_import" ] || fail "в /status нет importedAt"

grep -q 'OpenMapTiles' <<<"$(fetch "$BASE/tiles/tiles.json")" || fail "в tiles.json нет атрибуции OpenMapTiles"
grep -qi '^etag:' <<<"$(fetch -I "$BASE/tiles/basemap.pmtiles")" || fail "basemap.pmtiles без ETag"
grep -q 'OpenMapTiles' <<<"$(fetch "$BASE/tiles/style.json")" || fail "стиль без атрибуции"
echo "OK: тайлы и атрибуция"

# Обновление по расписанию (раз в минуту) должно завершиться успешно и сменить importedAt.
for _ in $(seq 1 60); do
    status="$(fetch "$BASE/status")"
    imported="$(sed -n 's/.*"importedAt":"\([^"]*\)".*/\1/p' <<<"$status")"
    if grep -q '"state":"error"' <<<"$status"; then
        fail "обновление по расписанию завершилось ошибкой: $status"
    fi
    if [ -n "$imported" ] && [ "$imported" != "$first_import" ] && grep -q '"state":"ok"' <<<"$status"; then
        echo "OK: обновление по расписанию ($imported)"
        grep -q '"status":"ok"' <<<"$(fetch "$BASE/health")" || fail "после обновления сервис не готов"
        echo "Все e2e-проверки пройдены"
        exit 0
    fi
    sleep 5
done
fail "обновление по расписанию не завершилось за 5 минут"
