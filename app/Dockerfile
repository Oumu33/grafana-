FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY go.mod ./
# 注释：为简化 demo，go.sum 由 go mod tidy 自动生成，无需手动 COPY
RUN go mod tidy
COPY . .
RUN go build -o main .

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/main .
CMD ["./main"]
