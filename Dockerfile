# syntax=docker/dockerfile:1.7

# Multi-stage para que la imagen final quede chica. Cache de modulos aparte
# asi no se invalida cada vez que toco un .go.
FROM golang:1.23-alpine AS build
WORKDIR /src

# tini para manejar bien las senales al container
RUN apk add --no-cache ca-certificates git tzdata

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

# Build estatico, sin cgo, trimpath para que el binario sea reproducible.
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags "-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.buildDate=${BUILD_DATE}" \
      -o /out/pulso-api ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=build /out/pulso-api /app/pulso-api
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

ENV TZ=America/Argentina/Buenos_Aires
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/pulso-api"]
