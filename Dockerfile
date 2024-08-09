FROM golang:alpine AS builder

WORKDIR /app

ADD . /app

RUN go mod download
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /app/docker-exporter

FROM scratch

COPY --from=builder /app/docker-exporter /
COPY --from=builder /app/tool/du .

ENTRYPOINT ["./docker-exporter"]
