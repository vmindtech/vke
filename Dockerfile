FROM golang:1.21.5-bullseye AS build-stage

WORKDIR /app

COPY . ./
RUN go mod download

RUN go build -o vke-application ./cmd/api

FROM gcr.io/distroless/base-debian11 AS build-release-stage

WORKDIR /

COPY --from=build-stage /app/vke-application /vke-application

EXPOSE 8080


ENTRYPOINT ["/vke-application"]