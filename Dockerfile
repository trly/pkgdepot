ARG GO_IMAGE=golang:1.27-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125
ARG ARCHLINUX_IMAGE=archlinux:base@sha256:4bf33b21a715aac0b48ce6e9eaed4782a898eae96f88f5da3635572129c2584a

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
