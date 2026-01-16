# Build stage - based on working pattern from coredns-dockerdiscovery
ARG GOLANG_VERS=1.21
ARG ALPINE_VERS=3.19
ARG COREDNS_VERS=1.11.1

FROM golang:${GOLANG_VERS}-alpine${ALPINE_VERS} AS builder

ARG COREDNS_VERS

RUN apk add --no-cache git

# Download CoreDNS as a module
RUN go mod download github.com/coredns/coredns@v${COREDNS_VERS}

# Work in the CoreDNS module directory
WORKDIR /go/pkg/mod/github.com/coredns/coredns@v${COREDNS_VERS}

# Make the module directory writable (module cache is read-only by default)
RUN chmod -R u+w .

# Download CoreDNS dependencies
RUN go mod download

# Copy plugin source
COPY --chmod=0755 . /src/ztdns

# Add ztdns plugin to plugin.cfg (after cache plugin)
RUN sed -i '/^cache:cache/a ztdns:ztdns' plugin.cfg

# Add replace directive and require the local module
RUN go mod edit -require ztdns@v0.0.0 && \
    go mod edit -replace ztdns@v0.0.0=/src/ztdns

# Generate and build
RUN go generate coredns.go
RUN go build -mod=mod -o /coredns

# Runtime stage
FROM alpine:${ALPINE_VERS}

RUN apk add --no-cache ca-certificates

COPY --from=builder /coredns /coredns

EXPOSE 53 53/udp

VOLUME ["/etc/coredns"]

ENTRYPOINT ["/coredns"]
CMD ["-conf", "/etc/coredns/Corefile"]
