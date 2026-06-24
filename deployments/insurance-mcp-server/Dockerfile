FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go mod init insurance-mcp-server && go build -o insurance-mcp-server .

FROM alpine:3.19
COPY --from=builder /app/insurance-mcp-server /insurance-mcp-server
EXPOSE 8090
ENTRYPOINT ["/insurance-mcp-server"]
