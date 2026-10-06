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

# pmtiles CLI (Protomaps) — версия закреплена для воспроизводимости.
FROM golang:1.27-bookworm AS tools
RUN go install github.com/protomaps/go-pmtiles@v1.31.2 \
 && cp /go/bin/* /out-pmtiles

# Апплаенс: PostgreSQL + PostGIS + приложение + инструменты импорта/тайлов.
# Внутреннее устройство (БД, креды, утилиты) наружу не выставлено — только ENV.
FROM postgres:16-bookworm

RUN apt-get update \
 && apt-get install -y --no-install-recommends postgresql-16-postgis-3 osm2pgsql ca-certificates curl \
 && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/slup-geo /usr/local/bin/slup-geo
COPY --from=tools /out-pmtiles /usr/local/bin/pmtiles
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
