FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /main ./cmd/main
RUN CGO_ENABLED=0 GOOS=linux go build -o /local ./cmd/local

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=builder /main /app/main
COPY --from=builder /local /app/local
EXPOSE 9001
ENTRYPOINT ["/app/main"]
