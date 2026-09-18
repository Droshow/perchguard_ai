FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY main.go .
RUN go mod init healthcare-mcp-server && go build -o healthcare-mcp-server .

FROM alpine:3.19
COPY --from=builder /app/healthcare-mcp-server /healthcare-mcp-server
EXPOSE 8091
ENTRYPOINT ["/healthcare-mcp-server"]
