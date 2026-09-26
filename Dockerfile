# Multi-stage build for the Go services (monitor + heartbeatchecker).
# The runtime image is minimal but includes CA certificates so HTTPS
# probes work, and tzdata for correct timestamps.

FROM golang:1.26-alpine AS build
WORKDIR /src

# Cache dependencies first.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/monitor ./cmd/monitor \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/heartbeatchecker ./cmd/heartbeatchecker

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 monitor
COPY --from=build /out/monitor /usr/local/bin/monitor
COPY --from=build /out/heartbeatchecker /usr/local/bin/heartbeatchecker

USER monitor
EXPOSE 8080
ENTRYPOINT ["monitor"]
