# syntax=docker/dockerfile:1
#
# Клиент MinIO (mc), собранный из исходников, — замена образу minio/mc.
#
# MinIO заархивировала mc в конце 2025-го: образов больше нет ни на Docker Hub
# (pull access denied), ни на quay.io, а dl.min.io на любой бинарник отвечает
# 410 Gone. До 02.10.2026 всё держалось на образе, случайно оставшемся в кэше
# docker с 2025-09-07, — и свежий сервер падал на фазе infra bootstrap.sh
# (.scratch/deploy-fresh-server/01-minio-mc-image-gone.md).
#
# Коммит — ровно тот, что сидел в последнем кэшированном minio/mc:latest
# (`mc --version`: RELEASE.2025-08-13T08-35-41Z, commit-id 7394ce0d…), поэтому
# поведение mirror (сравнение по mtime, флаги) не меняется ни на байт; грабли
# из скилла restore-prod остаются в силе как есть. Исходник берётся из Go
# module proxy, а не с GitHub: прокси хранит модули бессрочно, и архивный
# репозиторий, даже исчезнув, сборку не сломает. Go 1.24.6 — тот же, каким был
# собран прежний образ.
#
# Собирает его mc_run (scripts/lib.sh) сам, если образа нет; на живой боевой
# образ привозит release.sh вместе с остальными.

FROM golang:1.24.6-alpine AS build
ARG MC_MODULE_VERSION=v0.0.0-20250813083541-7394ce0dd2a8
ARG MC_RELEASE=RELEASE.2025-08-13T08-35-41Z
ARG MC_COMMIT=7394ce0dd2a80935aded936b09fa12cbb3cb8096
# Без ldflags `mc --version` печатает DEVELOPMENT.GOGET, и по образу не видно,
# какой это релиз. Имена переменных — из cmd/build-constants.go самого mc;
# ShortCommitID там считается из CommitID при старте, задавать его не нужно.
RUN CGO_ENABLED=0 GOBIN=/out go install \
      -trimpath \
      -ldflags "-s -w \
        -X github.com/minio/mc/cmd.Version=2025-08-13T08:35:41Z \
        -X github.com/minio/mc/cmd.CopyrightYear=2025 \
        -X github.com/minio/mc/cmd.ReleaseTag=${MC_RELEASE} \
        -X github.com/minio/mc/cmd.CommitID=${MC_COMMIT}" \
      github.com/minio/mc@${MC_MODULE_VERSION}

FROM alpine:3.22
# ca-certificates — на случай https-хоста в MC_HOST_*; сегодня mc_run ходит
# в SeaweedFS по http внутри docker-сети.
RUN apk add --no-cache ca-certificates
COPY --from=build /out/mc /usr/bin/mc
# mc_run запускает контейнер под uid хозяина с HOME=/tmp: своего /etc/passwd у
# такого uid нет, конфиг mc ложится в $HOME/.mc. /tmp у alpine общий на запись.
ENTRYPOINT ["mc"]
