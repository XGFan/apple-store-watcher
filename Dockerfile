FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w" -o /out/apple-store-watcher .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 app
COPY --from=builder /out/apple-store-watcher /usr/local/bin/apple-store-watcher
USER app
ENTRYPOINT ["/usr/local/bin/apple-store-watcher"]
CMD ["-c", "/etc/apple-store-watcher/config.yaml"]
