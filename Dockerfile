FROM golang:1.23-bullseye AS build-stage

WORKDIR /app

COPY . ./
RUN go mod download

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o vke-application ./cmd/api

FROM alpine:3.21.3 AS build-release-stage

WORKDIR /

COPY --from=build-stage /app/vke-application /vke-application
COPY --from=build-stage /app/locale /locale
COPY --from=build-stage /app/scripts/rke2-init-sh.tpl  /scripts/rke2-init-sh.tpl

EXPOSE 80


ENTRYPOINT ["/vke-application"]