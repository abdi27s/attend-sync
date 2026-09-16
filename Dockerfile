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

# Certificates are handy for future HTTPS webhooks; tzdata keeps
# device timestamps rendering in the right zone.
RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /app/attend-sync .

ENV PORT=8080

EXPOSE 8080

CMD ["./attend-sync"]