# syntax=docker/dockerfile:1

# --- build ---
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/freetube-sync ./cmd/freetube-sync
# Distroless has no shell/chown, so pre-create /data with the nonroot
# image's UID/GID (65532) here and copy it over with --chown below. Docker
# seeds a fresh named volume from the image's content at the mount path on
# first use — ownership included — so no init container is needed.
RUN mkdir /data && chown 65532:65532 /data

# --- final ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build --chown=65532:65532 /data /data
COPY --from=build /out/freetube-sync /usr/local/bin/freetube-sync
EXPOSE 8080
VOLUME /data
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/freetube-sync"]
CMD ["serve"]
