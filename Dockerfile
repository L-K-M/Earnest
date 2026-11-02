# syntax=docker/dockerfile:1

# Global defaults keep a bare `docker build` (scripts/build.sh --docker,
# `docker compose build`) from stamping empty versions or a spec-invalid empty
# created date; the release workflow overrides them via --build-arg. Stages
# re-declare the names bare to inherit these values. Epoch is the
# reproducible-builds "unset".
ARG VERSION=dev REVISION=unknown CREATED=1970-01-01T00:00:00Z

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ENV GOTOOLCHAIN=local
ARG TARGETOS TARGETARCH VERSION REVISION
WORKDIR /src
# No go.sum is copied: the scaffold is stdlib-only and the file does not
# exist yet. Add it back to this COPY when the first dependency lands.
COPY go.mod ./
COPY cmd/ cmd/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.revision=${REVISION}" \
      -o /out/earnest ./cmd/earnest

FROM alpine:3.22
ARG VERSION REVISION CREATED
LABEL org.opencontainers.image.title="Earnest" \
      org.opencontainers.image.description="Self-hosted RSS reader whose agent surface is governed: own identity, allowlist, budget, revert log" \
      org.opencontainers.image.url="https://github.com/L-K-M/Earnest" \
      org.opencontainers.image.documentation="https://github.com/L-K-M/Earnest/tree/main/docs" \
      org.opencontainers.image.source="https://github.com/L-K-M/Earnest" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.vendor="L-K-M" \
      org.opencontainers.image.licenses="Unlicense" \
      org.opencontainers.image.base.name="docker.io/library/alpine:3.22"
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/earnest /usr/local/bin/earnest
COPY LICENSE /usr/share/doc/earnest/LICENSE
ENV EARNEST_HTTP_ADDR=:8080
EXPOSE 8080
# The binary doubles as the liveness probe — no curl in this image.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/earnest", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/earnest"]
CMD ["serve"]
