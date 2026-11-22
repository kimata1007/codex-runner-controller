# Test the actual runner-agent HTTP and JSON-RPC implementation, replacing
# only its Codex CLI child with a deterministic local JSON-RPC/stdio executable.
FROM golang:1.24.12-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/runner-agent-entrypoint ./cmd/runner-agent
RUN CGO_ENABLED=0 go build -trimpath -o /out/codex ./tests/e2e/fake-app-server

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/runner-agent-entrypoint /runner-agent-entrypoint
COPY --from=build /out/codex /usr/local/bin/codex
ENV PATH=/usr/local/bin:/usr/bin:/bin
USER 65532:65532
ENTRYPOINT ["/runner-agent-entrypoint"]
