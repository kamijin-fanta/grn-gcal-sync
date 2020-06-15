FROM golang:1.14 as builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
RUN CGO_ENABLED=0 go build -o grn-gcal-sync .


FROM alpine:3.11

RUN apk add --no-cache ca-certificates && update-ca-certificates
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENV SSL_CERT_DIR=/etc/ssl/certs

COPY --from=builder /app/grn-gcal-sync /grn-gcal-sync
RUN mkdir /gen_configs
CMD ["/grn-gcal-sync"]
