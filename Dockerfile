FROM node:24-bookworm-slim AS frontend
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-bookworm AS backend
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY api/ api/
RUN CGO_ENABLED=0 go build -trimpath -o /hostops ./cmd/hostops && \
    CGO_ENABLED=0 go build -trimpath -o /hostops-agent ./cmd/hostops-agent && \
    CGO_ENABLED=0 go build -trimpath -o /hostops-watcher ./cmd/hostops-watcher

FROM alpine:3.23
RUN apk add --no-cache ca-certificates curl docker-cli openssh-client coreutils iproute2
COPY --from=backend /hostops /hostops-agent /hostops-watcher /usr/local/bin/
COPY --from=frontend /app/web/dist /opt/hostops/web
WORKDIR /opt/hostops
ENTRYPOINT ["hostops-agent"]
CMD ["--config", "/etc/hostops/sites.json", "--db", "/var/lib/hostops/history.db", "--web", "/opt/hostops/web"]
