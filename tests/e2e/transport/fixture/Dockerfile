FROM golang:1.24.12-bookworm@sha256:322a794c4289ad86e3043e5d9f16ead790463658a1bd2c3983427f02983ab978 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/fixture ./tests/e2e/transport/fixture

FROM scratch
COPY --from=build /out/fixture /fixture
USER 65532:65532
ENTRYPOINT ["/fixture"]
