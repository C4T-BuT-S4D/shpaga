FROM golang:1.23-alpine

WORKDIR /app
COPY . ./

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
        go build \
            -trimpath \
            -ldflags="-s -w" \
            -o api \
            ./cmd/api/main.go

CMD ["./api"]
