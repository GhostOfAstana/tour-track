# Сборка не требует сторонних модулей Go.
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /tour-track .

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 app
COPY --from=builder /tour-track /tour-track
USER app
ENV PORT=8080
EXPOSE 8080
ENTRYPOINT ["/tour-track"]
