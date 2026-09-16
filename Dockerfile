# Build stage
FROM golang:1.27.1-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
    -o attend-sync \
    ./cmd/server


# Runtime stage
FROM alpine:3.22

WORKDIR /app

COPY --from=builder /app/attend-sync .

EXPOSE 8080

CMD ["./attend-sync"]