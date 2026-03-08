FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /main ./cmd/main
RUN CGO_ENABLED=0 GOOS=linux go build -o /local ./cmd/local

FROM gcr.io/distroless/static-debian12
COPY --from=builder /main /main
COPY --from=builder /local /local
COPY --from=builder /app/internal/cache/cards_gen.json /app/internal/cache/cards_gen.json
EXPOSE 9001
ENTRYPOINT ["/main"]
