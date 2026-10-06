# syntax=docker/dockerfile:1

# Сборка приложения. Базовые образы закреплены по digest (обновляет Dependabot).
FROM golang:1.27-bookworm@sha256:a4f46dc39c6b0359a3e1ed86ef14d01b374cc808649679dd5fca2290e6d54202 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT} -X main.date=${DATE}" \
    -o /out/slup-geo ./cmd/slup-geo

# tilemaker v3.2.0 — генератор векторных тайлов из OSM PBF.
# Версия и sha256 архива закреплены; v3 умеет писать .pmtiles напрямую (без промежуточного MBTiles).
FROM debian:bookworm@sha256:2c037a04925515fdd6ea85ea14a682d0e79931f5e9f5d07b6dbfc6ba12f9e858 AS tilemaker-build
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      build-essential ca-certificates curl \
      libboost-dev libboost-filesystem-dev libboost-program-options-dev libboost-system-dev \
      lua5.3 liblua5.3-dev libshp-dev libsqlite3-dev rapidjson-dev \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL "https://github.com/systemed/tilemaker/archive/refs/tags/v3.2.0.tar.gz" -o /tmp/tilemaker.tar.gz \
 && echo "7f8a49412e6e0fbe8d9230ccf8ec8a9be442c9d765d4bd6a63465ca82c087f75  /tmp/tilemaker.tar.gz" | sha256sum -c - \
 && tar -xzf /tmp/tilemaker.tar.gz -C /tmp \
 && mv /tmp/tilemaker-3.2.0 /src \
 && rm /tmp/tilemaker.tar.gz
WORKDIR /src
RUN make -j"$(nproc)" \
 && cp tilemaker /out-tilemaker

# Апплаенс: PostgreSQL + PostGIS + приложение + импорт OSM + сборка тайлов.
# Внутреннее устройство (БД, креды, утилиты) наружу не выставлено — только ENV.
FROM postgres:16-bookworm@sha256:0ea6700a3b4f0ae6ce746519073558aed4d88a79d8d07622a9a644946c7319c4

# osm2pgsql — импорт OSM в PostGIS (поиск); runtime-библиотеки для tilemaker.
# apt-пакеты намеренно не пиннятся по версиям: так образ получает security-обновления
# при пересборке (версии и так зафиксированы релизом Debian bookworm).
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      postgresql-16-postgis-3 osm2pgsql ca-certificates curl tzdata \
      liblua5.3-0 libshp2 libsqlite3-0 \
      libboost-filesystem1.74.0 libboost-program-options1.74.0 libboost-iostreams1.74.0 libboost-system1.74.0 \
 && rm -rf /var/lib/apt/lists/*

# Непривилегированный пользователь для serve/update: утилиты не работают от root.
RUN groupadd -g 10001 slup \
 && useradd -u 10001 -g slup -M -d /nonexistent -s /usr/sbin/nologin slup

COPY --from=build /out/slup-geo /usr/local/bin/slup-geo
COPY --from=tilemaker-build /out-tilemaker /usr/local/bin/tilemaker
COPY tiles/tilemaker/ /usr/local/share/tilemaker/
COPY tiles/web/ /usr/local/share/slup-geo/
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

ENV DATA_DIR=/data \
    PGDATA=/data/pgdata \
    HTTP_ADDR=:8080 \
    POSTGRES_DB=geo_db \
    POSTGRES_USER=geo_user \
    PGHOST=127.0.0.1 \
    PGPORT=5432 \
    PGDATABASE=geo_db \
    PGUSER=geo_user

EXPOSE 8080
VOLUME ["/data"]

# start-period с запасом: первый импорт большого региона может идти долго.
HEALTHCHECK --interval=15s --timeout=5s --start-period=1800s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
