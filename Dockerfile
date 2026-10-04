# Build the frontend.
# The build stages run on the native build platform; only the final image is
# per-target, so multi-arch builds need no emulation.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN mkdir -p ../internal/webui/dist && npm run build

# Build the server with the frontend embedded.
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/internal/webui/dist/ internal/webui/dist/
ARG VERSION=dev
ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /drawered ./cmd/drawered \
 && go build -trimpath -ldflags "-s -w" -o /drawered-import ./cmd/drawered-import \
 && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /drawered /drawered
COPY --from=server /drawered-import /drawered-import
# Owned by nonroot so that a fresh named volume mounted here is writable.
COPY --from=server --chown=nonroot:nonroot /data /data
ENV DRAWERED_LISTEN=:8080 DRAWERED_DATA_DIR=/data
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/drawered"]
CMD ["serve"]
