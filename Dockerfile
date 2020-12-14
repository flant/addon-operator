# Prebuilt libjq.
FROM --platform=${TARGETPLATFORM:-linux/amd64} flant/jq:b6be13d5-musl as libjq

# Go builder.
FROM --platform=${TARGETPLATFORM:-linux/amd64} golang:1.15-alpine3.12 AS builder

ARG appVersion=latest
RUN apk --no-cache add git ca-certificates gcc musl-dev

# Cache-friendly download of go dependencies.
ADD go.mod go.sum /app/
WORKDIR /app
RUN go mod download

COPY --from=libjq /libjq /libjq
ADD . /app
WORKDIR /app

RUN git submodule update --init --recursive && \
    shellOpVer=$(go list -m all | grep shell-operator | cut -d' ' -f 2-) \
    CGO_ENABLED=1 \
    CGO_CFLAGS="-I/libjq/include" \
    CGO_LDFLAGS="-L/libjq/lib" \
    GOOS=linux \
    go build -ldflags="-linkmode external -extldflags '-static' -s -w -X 'github.com/flant/shell-operator/pkg/app.Version=$shellOpVer' -X 'github.com/flant/addon-operator/pkg/app.Version=$appVersion'" \
             -tags='release' \
             -o addon-operator \
             ./cmd/addon-operator

# build final image
FROM --platform=${TARGETPLATFORM:-linux/amd64} alpine:3.12

# kubectl url has no variant (v7)
# helm url has dashes and no variant (v7)
RUN apk --no-cache add ca-certificates jq bash sed tini && \
    kubectlArch=$(echo ${TARGETPLATFORM:-linux/amd64} | sed 's/\/v7//') && \
    wget https://storage.googleapis.com/kubernetes-release/release/v1.19.4/bin/${kubectlArch}/kubectl -O /bin/kubectl && \
    chmod +x /bin/kubectl && \
    helmArch=$(echo ${TARGETPLATFORM:-linux/amd64} | sed 's/\//-/g;s/-v7//') && \
    wget https://get.helm.sh/helm-v3.4.1-${helmArch}.tar.gz -O /helm.tgz && \
    tar -z -x -C /bin -f /helm.tgz --strip-components=1 ${helmArch}/helm && \
    rm -f /helm.tgz

COPY --from=builder /app/addon-operator /
COPY --from=builder /app/shell-operator/frameworks/shell/ /framework/shell/
WORKDIR /

ENV MODULES_DIR /modules
ENV GLOBAL_HOOKS_DIR /global-hooks
ENTRYPOINT ["/sbin/tini", "--", "/addon-operator"]
