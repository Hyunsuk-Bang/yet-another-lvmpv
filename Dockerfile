FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o lvmpv .

FROM ubuntu:22.04
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      lvm2 \
      e2fsprogs \
      util-linux \
    && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/lvmpv /usr/local/bin/lvmpv
ENTRYPOINT ["/usr/local/bin/lvmpv"]
