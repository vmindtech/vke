FROM golang:1.22-bullseye AS build-stage

WORKDIR /app

COPY . ./
RUN go mod download

RUN go build -o vke-application ./cmd/api

FROM gcr.io/distroless/base-debian11 AS build-release-stage

WORKDIR /

COPY --from=build-stage /app/vke-application /vke-application
COPY --from=build-stage /app/locale /locale
COPY --from=build-stage /app/scripts/rke2-init-sh.tpl  /scripts/rke2-init-sh.tpl

EXPOSE 80


ENTRYPOINT ["/vke-application"]