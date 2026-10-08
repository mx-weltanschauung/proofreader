# syntax=docker/dockerfile:1

FROM golang:1.25 AS build
ARG COMMIT=unknown
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-X main.Commit=$COMMIT" -o /out/server ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends poppler-utils djvulibre-bin ca-certificates \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/server /app/server
COPY --from=build /out/migrate /app/migrate
COPY internal/database/migrations /app/internal/database/migrations
EXPOSE 8080
CMD ["/app/server"]
