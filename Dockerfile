# build libjq
FROM ubuntu:18.04 AS libjq
ENV DEBIAN_FRONTEND=noninteractive \
    DEBCONF_NONINTERACTIVE_SEEN=true \
    LC_ALL=C.UTF-8 \
    LANG=C.UTF-8

RUN apt-get update && \
    apt-get install -y git ca-certificates && \
    git clone https://github.com/flant/libjq-go /libjq-go && \
    cd /libjq-go && \
    git submodule update --init && \
    /libjq-go/scripts/install-libjq-dependencies-ubuntu.sh && \
    /libjq-go/scripts/build-libjq-static.sh /libjq-go /out


# build addon-operator binary linked with libjq
FROM golang:1.12 AS addon-operator
ARG appVersion=latest

# Cache-friendly download of go dependencies.
ADD go.mod go.sum /addon-operator/
WORKDIR /addon-operator
RUN go mod download

COPY --from=libjq /out/build /build
ADD . /addon-operator
WORKDIR /addon-operator

RUN ./go-build.sh $appVersion


# build final image
FROM ubuntu:18.04
RUN apt-get update && \
    apt-get install -y ca-certificates wget jq && \
    rm -rf /var/lib/apt/lists && \
    wget https://storage.googleapis.com/kubernetes-release/release/v1.13.5/bin/linux/amd64/kubectl -O /bin/kubectl && \
    chmod +x /bin/kubectl && \
    wget https://storage.googleapis.com/kubernetes-helm/helm-v2.14.3-linux-amd64.tar.gz -O /helm.tgz && \
    tar -z -x -C /bin -f /helm.tgz --strip-components=1 linux-amd64/helm linux-amd64/tiller && \
    rm -f /helm.tgz && \
    helm init --client-only && \
    mkdir /hooks
COPY --from=addon-operator /addon-operator/addon-operator /
WORKDIR /
ENV MODULES_DIR /modules
ENV GLOBAL_HOOKS_DIR /global-hooks
ENTRYPOINT ["/addon-operator"]
