FROM golang:1.26-alpine AS builder
ENV GOWORK=off
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /app/server /app/server
COPY config/ /app/config/
ENV APP_ENV=production
EXPOSE 8082
CMD ["/app/server"]
