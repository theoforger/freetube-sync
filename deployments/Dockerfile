# syntax=docker/dockerfile:1

# --- build ---
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/freetube-sync ./cmd/freetube-sync

# --- final ---
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/freetube-sync /usr/local/bin/freetube-sync
EXPOSE 8080
VOLUME /data
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/freetube-sync"]
CMD ["serve"]
