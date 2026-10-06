# syntax=docker/dockerfile:1

# Сборка приложения.
FROM golang:1.27-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/slup-geo ./cmd/slup-geo

# tilemaker v3.2.0 — генератор векторных тайлов из OSM PBF.
# Версия закреплена; v3 умеет писать .pmtiles напрямую (без промежуточного MBTiles).
FROM debian:bookworm AS tilemaker-build
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      build-essential ca-certificates curl \
      libboost-dev libboost-filesystem-dev libboost-program-options-dev libboost-system-dev \
      lua5.3 liblua5.3-dev libshp-dev libsqlite3-dev rapidjson-dev \
 && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL "https://github.com/systemed/tilemaker/archive/refs/tags/v3.2.0.tar.gz" -o /tmp/tilemaker.tar.gz \
 && tar -xzf /tmp/tilemaker.tar.gz -C /tmp \
 && mv /tmp/tilemaker-3.2.0 /src \
 && rm /tmp/tilemaker.tar.gz
WORKDIR /src
RUN make -j"$(nproc)" \
 && cp tilemaker /out-tilemaker

# Апплаенс: PostgreSQL + PostGIS + приложение + импорт OSM + сборка тайлов.
# Внутреннее устройство (БД, креды, утилиты) наружу не выставлено — только ENV.
FROM postgres:16-bookworm

# osm2pgsql — импорт OSM в PostGIS (поиск); runtime-библиотеки для tilemaker.
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      postgresql-16-postgis-3 osm2pgsql ca-certificates curl \
      liblua5.3-0 libshp2 libsqlite3-0 \
      libboost-filesystem1.74.0 libboost-program-options1.74.0 libboost-iostreams1.74.0 libboost-system1.74.0 \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/slup-geo /usr/local/bin/slup-geo
COPY --from=tilemaker-build /out-tilemaker /usr/local/bin/tilemaker
COPY tiles/ /usr/local/share/tilemaker/
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

HEALTHCHECK --interval=15s --timeout=5s --start-period=600s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
