#!/usr/bin/env bash
# Дымовой тест гео-сервиса по запущенному контейнеру.
# Использование: ./scripts/smoke.sh [http://localhost:8083]
set -euo pipefail

BASE_URL="${1:-http://localhost:8083}"

check() {
    local name="$1"
    local url="$2"
    local expect="${3:-}"
    local body
    body="$(curl -fsS "$url")"
    if [ -n "$expect" ] && ! grep -q "$expect" <<<"$body"; then
        echo "ОШИБКА: $name — в ответе нет '$expect': $body" >&2
        exit 1
    fi
    echo "OK: $name"
}

check "health" "$BASE_URL/health" '"status":"ok"'
check "point: дом с адресом" "$BASE_URL/v1/point?lat=52.3955063&lon=30.9607992" "Григория Денисенко"
check "point: зоны без дома" "$BASE_URL/v1/point?lat=52.3021783&lon=30.8780772" "Бобовичский"
check "suggest: улица" "$BASE_URL/v1/suggest?q=%D0%91%D0%BE%D1%80%D0%BE%D0%B4%D0%B8%D0%BD%D0%B0" '"items"'
check "suggest: город" "$BASE_URL/v1/suggest?q=%D0%93%D0%BE%D0%BC%D0%B5%D0%BB%D1%8C" "CITY"
check "areas: зона" "$BASE_URL/v1/areas/W-3628814?simplify=display" '"geometry"'
check "tiles: манифест" "$BASE_URL/tiles/tiles.json" '"files"'

echo "Все проверки пройдены: $BASE_URL"
