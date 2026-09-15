FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . ./
RUN CGO_ENABLED=0 go test ./... && \
    CGO_ENABLED=0 go build -o /out/leeclaw-core ./cmd/graph-api

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/leeclaw-core /usr/local/bin/leeclaw-core
COPY configs/semantic-overlay.json /etc/leeclaw/semantic-overlay.json
EXPOSE 8090
ENTRYPOINT ["leeclaw-core"]
