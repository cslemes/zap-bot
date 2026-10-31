FROM golang:1.27-bookworm AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -o /zap-bot .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home zapbot \
    && mkdir /data && chown zapbot:zapbot /data
WORKDIR /app
COPY --from=builder /zap-bot /app/zap-bot
ENV DATA_DIR=/data HTTP_ADDR=0.0.0.0:8080
USER zapbot
EXPOSE 8080
ENTRYPOINT ["/app/zap-bot"]
