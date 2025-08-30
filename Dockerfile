FROM  golang:1.24 AS builder


WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o zap-bot main.go

#FROM scratch
#FROM gcr.io/distroless/static-debian12
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /app/zap-bot .
COPY login-store.db .
#EXPOSE 8080
ENTRYPOINT ["./zap-bot"]