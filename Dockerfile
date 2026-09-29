ARG GO_IMAGE=golang:1.27-alpine@sha256:4cb7ac979db5fcc41cae44b2227ba5ab8a51e8807f40d9ba4dee20a0ad960b5b
ARG ARCHLINUX_IMAGE=archlinux:base@sha256:b21322c663be387c0ed9cbc7bbbfe18e41633ad4e7b7c77cfad45f128be20040

FROM ${GO_IMAGE} AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /pkgdepot ./cmd/pkgdepot

FROM ${ARCHLINUX_IMAGE}
COPY --from=build /pkgdepot /usr/local/bin/pkgdepot
VOLUME ["/var/lib/pkgdepot"]
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/pkgdepot"]
CMD ["serve"]
