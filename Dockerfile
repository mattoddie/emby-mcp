# syntax=docker/dockerfile:1

# The binary is static Go, so the build stage runs on the build machine's own
# platform and cross-compiles: multi-arch builds don't need emulation.
FROM --platform=$BUILDPLATFORM golang:1.27@sha256:ec47ea3725fea0cc88f6de94919aef19f40f12fc1ec0e2d1b3426dbf15ecca04 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go test ./...
# Release builds pass the version from the Git tag (e.g. 1.2.3).
ARG VERSION=0.0.0-dev
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/mattoddie/emby-mcp/internal/tools.Version=${VERSION}" \
      -o /out/emby-mcp ./cmd/emby-mcp

# Distroless: no shell, no package manager, just CA certificates, tzdata and a
# non-root user (65532), so there is very little for CVE scanners to find.
# emby-mcp keeps no state, so it needs no volume.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
LABEL org.opencontainers.image.title="emby-mcp" \
      org.opencontainers.image.description="MCP server for Emby media servers" \
      org.opencontainers.image.source="https://github.com/mattoddie/emby-mcp" \
      org.opencontainers.image.documentation="https://emby-mcp.mattoddie.dev/docs/" \
      org.opencontainers.image.licenses="MIT" \
      io.modelcontextprotocol.server.name="io.github.mattoddie/emby-mcp"
COPY --from=build /out/emby-mcp /emby-mcp
ENV MCP_TRANSPORT=http \
    MCP_HTTP_HOST=0.0.0.0 \
    MCP_HTTP_PORT=8080
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 CMD ["/emby-mcp", "healthcheck"]
ENTRYPOINT ["/emby-mcp"]
