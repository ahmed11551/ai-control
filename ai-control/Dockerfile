FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/ai-control ./cmd/ai-control
FROM alpine:3.20
RUN apk add --no-cache tzdata
RUN adduser -D -u 10001 ai
USER ai
COPY --from=build /out/ai-control /ai-control
EXPOSE 8091
ENTRYPOINT ["/ai-control"]
