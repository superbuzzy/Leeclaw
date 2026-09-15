FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY cmd/auth-gateway ./cmd/auth-gateway
RUN test -z "$(gofmt -l ./cmd/auth-gateway)" && \
    CGO_ENABLED=0 go test ./cmd/auth-gateway && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/leeclaw-auth-gateway ./cmd/auth-gateway

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=build /out/leeclaw-auth-gateway /usr/local/bin/leeclaw-auth-gateway
EXPOSE 18789 18790
USER 65532:65532
ENTRYPOINT ["leeclaw-auth-gateway"]
