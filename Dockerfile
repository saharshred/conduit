# One image, all Go binaries — docker-compose.yml picks which to run per
# service via `entrypoint:`.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/mockbanks ./cmd/mockbanks \
 && go build -o /out/normalizer ./cmd/normalizer \
 && go build -o /out/ingestor ./cmd/ingestor \
 && go build -o /out/gen-dataset ./scripts/gen-dataset

FROM alpine:3.20
COPY --from=build /out/ /usr/local/bin/
