FROM golang:1.24.12-bookworm AS builder
WORKDIR /src
COPY tests/e2e/fake-agent/main.go ./main.go
RUN CGO_ENABLED=0 go build -trimpath -o /out/runner-agent-entrypoint ./main.go

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/runner-agent-entrypoint /runner-agent-entrypoint
USER 65532:65532
ENTRYPOINT ["/runner-agent-entrypoint"]
