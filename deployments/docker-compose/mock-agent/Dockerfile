FROM golang:1.23-alpine AS builder

WORKDIR /src
COPY main.go .
RUN go mod init mock-agent && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /mock-agent .

FROM scratch
COPY --from=builder /mock-agent /mock-agent
ENTRYPOINT ["/mock-agent"]
