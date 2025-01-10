FROM golang:1.23 AS builder

RUN mkdir /data

WORKDIR /app
COPY . /app
RUN CGO_ENABLED=0 GOOS=linux go build -a -ldflags '-extldflags "-static"' -trimpath -ldflags=-buildid= -o main .

FROM ghcr.io/greboid/dockerbase/nonroot:1.20250110.0

COPY --from=builder --chown=65532 /data /data

COPY --from=builder /app/main /githubmirror
CMD ["/githubmirror"]
