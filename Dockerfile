# syntax=docker/dockerfile:1

# Сборка приложения. Базовые образы закреплены по digest (обновляет Dependabot).
FROM golang:1.27-bookworm@sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61 AS build
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
# Лицензии всего, что вкомпилировано в бинарник (MIT/BSD требуют уведомлений при бинарной
# поставке): стандартная библиотека Go + каждый модуль из графа сборки. Модуль без файла
# лицензии валит сборку — новая зависимость не уедет в образ без уведомления.
RUN set -eu; \
    mkdir -p /out/licenses/go; \
    cp "$(go env GOROOT)/LICENSE" /out/licenses/go/golang-stdlib-LICENSE; \
    if [ -f "$(go env GOROOT)/PATENTS" ]; then cp "$(go env GOROOT)/PATENTS" /out/licenses/go/golang-stdlib-PATENTS; fi; \
    go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}}@{{.Version}} {{.Dir}}{{end}}{{end}}' ./cmd/slup-geo \
      | sort -u | while read -r mod dir; do \
        mkdir -p "/out/licenses/go/$mod"; \
        found=0; \
        for f in "$dir"/LICENSE* "$dir"/LICENCE* "$dir"/COPYING* "$dir"/NOTICE* "$dir"/PATENTS*; do \
          if [ -f "$f" ]; then cp "$f" "/out/licenses/go/$mod/"; found=1; fi; \
        done; \
        if [ "$found" -ne 1 ]; then echo "нет файла лицензии у модуля $mod" >&2; exit 1; fi; \
      done

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
      libboost-filesystem1.74.0 libboost-program-options1.74.0 \
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

# Лицензии и уведомления: собственный код (MIT), сводка сторонних компонентов, лицензии
# Go-модулей и библиотек, вкомпилированных в tilemaker, а также точные версии пакетов
# Debian/PGDG (по ним находятся исходники GPL-компонентов — см. THIRD_PARTY_LICENSES.md).
COPY LICENSE THIRD_PARTY_LICENSES.md /usr/share/doc/slup-geo/
COPY licenses/tilemaker-third-party.txt /usr/share/doc/tilemaker/THIRD_PARTY_NOTICES.txt
COPY tiles/tilemaker/LICENCE.txt /usr/share/doc/tilemaker/LICENCE.txt
COPY --from=build /out/licenses/go/ /usr/share/doc/slup-geo/go-licenses/
RUN dpkg-query -W -f '${Package}\t${Version}\t${source:Package}\t${source:Version}\n' \
      > /usr/share/doc/slup-geo/debian-packages.tsv

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
# Базовый образ postgres объявляет EXPOSE 5432 и VOLUME /var/lib/postgresql/data — отменить их
# в Dockerfile нельзя. PostgreSQL слушает только 127.0.0.1, данные — в /data/pgdata; анонимный
# volume остаётся пустым (удаляется вместе с контейнером через docker rm -v).
VOLUME ["/data"]

# start-period с запасом: первый импорт большого региона может идти долго.
HEALTHCHECK --interval=15s --timeout=5s --start-period=1800s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
