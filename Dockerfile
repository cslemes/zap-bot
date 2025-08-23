FROM  golang:alpine3.22 AS builder
RUN apk add --no-cache alpine-sdk

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 go build -o zap-bot cmd/whatsapp-bot/main.go

FROM scratch
WORKDIR /app
COPY --from=builder /app/zap-bot /app/zap-bot
COPY index.html index.html
COPY login-store.db login-store.db
EXPOSE 8080
ENTRYPOINT ["/app/zap-bot"]