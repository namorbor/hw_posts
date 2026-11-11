FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /blog-api .

FROM alpine:3.22
RUN addgroup -S app && adduser -S -G app app \
    && mkdir -p /app/data /app/logs && chown -R app:app /app
WORKDIR /app
COPY --from=builder /blog-api /app/blog-api
USER app
ENV PORT=8080 DATA_DIR=/app/data LOG_FILE=/app/logs/log.txt
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/health || exit 1
ENTRYPOINT ["/app/blog-api"]
