ARG GO_IMAGE=golang:1.26-alpine@sha256:28d89ee9cc0ff9fec75c82ca201e6bf7fdf9a679d4b7b24dfa04f2bb766bb468
ARG ARCHLINUX_IMAGE=archlinux:base@sha256:b0deabeb3d283da2c7f7dbf0eea051b7b2cd0554e0b737cc457fd21683bdcdd1

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
