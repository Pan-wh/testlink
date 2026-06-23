# Build stage
FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /server ./cmd/server/

# Runtime stage
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
ENV TZ=Asia/Shanghai
WORKDIR /app
COPY --from=builder /server .
COPY web/ ./web/
COPY config.yaml .
COPY ip2region_v4.xdb .
COPY ip2region_v6.xdb .
COPY GeoLite2-Country.mmdb .
COPY GeoLite2-ASN.mmdb .
EXPOSE 8080
ENTRYPOINT ["./server", "config.yaml"]
