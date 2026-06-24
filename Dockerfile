FROM golang:1.25-alpine AS builder

RUN apk add --no-cache ca-certificates

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /perchguard ./cmd/

FROM alpine:latest

RUN apk add --no-cache ca-certificates wget

WORKDIR /app

COPY --from=builder /perchguard /perchguard
COPY configs/ /etc/perchguard/

ENV PERCHGUARD_POLICY=/etc/perchguard/policies.yaml
ENV PERCHGUARD_ADDR=:8080

EXPOSE 8080 8443

ENTRYPOINT ["/perchguard"]
