FROM golang:1.27.1@sha256:512690a5660563b57d37ecc31129e7f136e831db2aed24a1dbeb8ad7380dc0fa AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY examples/shop ./examples/shop
ARG VERSION=v1
# Share compilation work with other local images and shop variants.
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /shop ./examples/shop
FROM scratch
COPY --from=build /shop /shop
USER 65532:65532
ENTRYPOINT ["/shop"]
