FROM golang:1.23-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod ./
RUN go mod download || true
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /api ./cmd/api

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && mkdir -p /app/data/inbox-media /app/data/ticket-files \
 && chown -R nobody:nobody /app/data
WORKDIR /app
COPY --from=build /api /app/api
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app/api"]
