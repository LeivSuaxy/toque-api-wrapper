FROM golang:1.27-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN mkdir -p /out/data \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/toque-api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/toque-api /toque-api
COPY --from=build --chown=nonroot:nonroot /out/data /data

VOLUME ["/data"]

ENV DATABASE_PATH=/data/toque.db \
    HTTP_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/toque-api"]
