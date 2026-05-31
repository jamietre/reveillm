FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o reveillm ./cmd/reveillm

FROM alpine:3.19
RUN apk add --no-cache ca-certificates bash
COPY --from=builder /app/reveillm /usr/local/bin/reveillm
ENTRYPOINT ["reveillm"]
CMD ["--config", "/etc/reveillm/config.yaml"]
