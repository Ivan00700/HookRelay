FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/hookrelay ./cmd/hookrelay \
    && CGO_ENABLED=0 go build -trimpath -o /out/dbcheck ./cmd/dbcheck \
    && CGO_ENABLED=0 go build -trimpath -o /out/loadgen ./cmd/loadgen \
    && CGO_ENABLED=0 go build -trimpath -o /out/receiver ./examples/receiver

FROM alpine:3.23
RUN addgroup -S hookrelay && adduser -S -G hookrelay hookrelay
WORKDIR /app
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/ /app/
USER hookrelay
EXPOSE 8080
ENTRYPOINT ["/app/hookrelay"]
