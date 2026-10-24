ARG GO_VERSION=1.27.1

FROM golang:${GO_VERSION} AS build
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ARG TARGETOS TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/server ./cmd/server

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/goose github.com/pressly/goose/v3/cmd/goose

FROM gcr.io/distroless/static-debian12:nonroot AS migrate
COPY --from=build /out/goose /goose
COPY migrations /migrations
ENTRYPOINT [ "/goose" ]

FROM gcr.io/distroless/static-debian12:nonroot AS server
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT [ "/server" ]
