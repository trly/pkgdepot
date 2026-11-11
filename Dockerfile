ARG GO_IMAGE=golang:1.27-alpine@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523
ARG ARCHLINUX_IMAGE=archlinux:base@sha256:b1c2fe6e717bfd5673af8fc217e1e1c3ce0dcbb1d53c3414c8425e7ce79fc6d9

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
