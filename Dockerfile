FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=secret,id=COMMON_GO_MODULES_FETCH \
    --mount=type=secret,id=BATTLE_GO_MODULES_FETCH \
    --mount=type=secret,id=SERVICES_GO_MODULES_FETCH \
    git config --global url."https://x-access-token:$(cat /run/secrets/COMMON_GO_MODULES_FETCH)@github.com/kenyamaneko/overload-party-common".insteadOf "https://github.com/kenyamaneko/overload-party-common" && \
    git config --global url."https://x-access-token:$(cat /run/secrets/BATTLE_GO_MODULES_FETCH)@github.com/kenyamaneko/overload-party-battle".insteadOf "https://github.com/kenyamaneko/overload-party-battle" && \
    git config --global url."https://x-access-token:$(cat /run/secrets/SERVICES_GO_MODULES_FETCH)@github.com/kenyamaneko/overload-party-card".insteadOf "https://github.com/kenyamaneko/overload-party-card" && \
    git config --global url."https://x-access-token:$(cat /run/secrets/SERVICES_GO_MODULES_FETCH)@github.com/kenyamaneko/overload-party-account".insteadOf "https://github.com/kenyamaneko/overload-party-account" && \
    git config --global url."https://x-access-token:$(cat /run/secrets/SERVICES_GO_MODULES_FETCH)@github.com/kenyamaneko/overload-party-shop".insteadOf "https://github.com/kenyamaneko/overload-party-shop" && \
    git config --global url."https://x-access-token:$(cat /run/secrets/SERVICES_GO_MODULES_FETCH)@github.com/kenyamaneko/overload-party-scenario".insteadOf "https://github.com/kenyamaneko/overload-party-scenario" && \
    GOPRIVATE=github.com/kenyamaneko/* go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /main ./cmd/main
RUN CGO_ENABLED=0 GOOS=linux go build -o /local ./cmd/local

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=builder /main /app/main
COPY --from=builder /local /app/local
EXPOSE 9001
ENTRYPOINT ["/app/main"]
