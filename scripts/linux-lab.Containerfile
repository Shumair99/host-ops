# Disposable systemd validation only; never install this image on a VPS.
FROM docker.io/library/debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl gcc libc6-dev systemd systemd-sysv procps iproute2 \
    && rm -rf /var/lib/apt/lists/*
RUN curl -fsSL https://go.dev/dl/go1.27.2.linux-amd64.tar.gz -o /tmp/go.tar.gz \
    && echo 'ecbadb99091a3f46e31f5f934b068b1864eafa7995211b39eaddf76996045fe5  /tmp/go.tar.gz' | sha256sum -c - \
    && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz
ENV PATH=/usr/local/go/bin:/usr/sbin:/usr/bin:/sbin:/bin
ENV GOTOOLCHAIN=local
# Runtime /tmp is a noexec tmpfs. Go must put executable test binaries in a
# separate private directory rather than weakening that mount's permissions.
RUN install -d -m 0700 /var/tmp/hostops-go-build
ENV GOTMPDIR=/var/tmp/hostops-go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Compile race dependencies while the build has network access. Runtime uses
# network=none; probes in the tests connect only to their own loopback fixtures.
RUN go test -race -run '^$' ./... \
    && CGO_ENABLED=0 go build -trimpath -o /opt/hostops-vps-agent ./cmd/hostops-vps-agent
CMD ["/sbin/init"]
