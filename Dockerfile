ARG GO_VERSION=1.27.3
FROM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0
COPY . .
# Download the dependencies pinned in go.mod and go.sum.
RUN go mod download && go vet ./... && go test ./... && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/dorm-hub ./cmd/api

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && addgroup -S -g 10001 dormhub && adduser -S -u 10001 -G dormhub dormhub && mkdir -p /app/data/uploads && chown -R dormhub:dormhub /app
WORKDIR /app
COPY --from=build /out/dorm-hub /app/dorm-hub
USER dormhub
EXPOSE 8080
ENTRYPOINT ["/app/dorm-hub"]
