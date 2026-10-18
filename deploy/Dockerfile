FROM golang:1.26-alpine AS builder
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server ./cmd/${SERVICE}

FROM alpine:3.21
ARG PORT
RUN apk add --no-cache ca-certificates wget
WORKDIR /app
COPY --from=builder /app/server /app/server
COPY config/ /app/config/
ENV APP_ENV=production
EXPOSE ${PORT}
CMD ["/app/server"]
