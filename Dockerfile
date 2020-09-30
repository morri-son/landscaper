#### BUILDER ####
FROM golang:1.15.0 AS builder

WORKDIR /go/src/github.com/gardener/landscaper
COPY . .

ARG EFFECTIVE_VERSION

RUN make install EFFECTIVE_VERSION=$EFFECTIVE_VERSION

#### BASE ####
FROM alpine:3.11.6 AS base

#### Helm Deployer Controller ####
FROM base as landscaper-controller

COPY --from=builder /go/bin/landscaper-controller /landscaper-controller

WORKDIR /

ENTRYPOINT ["/landscaper-controller"]

#### Container Deployer Controller ####
FROM base as container-deployer-controller

COPY --from=builder /go/bin/container-deployer-controller /container-deployer-controller

WORKDIR /

ENTRYPOINT ["/container-deployer-controller"]

#### Container Deployer Init ####
FROM base as container-deployer-init

COPY --from=builder /go/bin/container-deployer-init /container-deployer-init

WORKDIR /

ENTRYPOINT ["/container-deployer-init"]

#### Container Deployer wait ####
FROM base as container-deployer-wait

COPY --from=builder /go/bin/container-deployer-wait /container-deployer-wait

WORKDIR /

ENTRYPOINT ["/container-deployer-wait"]

#### Helm Deployer Controller ####
FROM base as helm-deployer-controller

COPY --from=builder /go/bin/helm-deployer-controller /helm-deployer-controller

WORKDIR /

ENTRYPOINT ["/helm-deployer-controller"]
