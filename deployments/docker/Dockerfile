FROM golang:1.21-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git make

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o server ./cmd/server

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

COPY --from=builder /app/server .
COPY --from=builder /app/configs ./configs
COPY --from=builder /app/migrations ./migrations

EXPOSE 8085 9090

ENV DB_PASSWORD=postgres
ENV REDIS_PASSWORD=
ENV JWT_SECRET=development-secret

CMD ["./server"]
