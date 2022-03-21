# Prebuilt libjq.
FROM --platform=${TARGETPLATFORM:-linux/amd64} flant/jq:b6be13d5-musl as libjq

# Go builder.
FROM --platform=${TARGETPLATFORM:-linux/amd64} golang:1.16-alpine3.15 AS builder

ARG appVersion=latest
RUN apk --no-cache add git ca-certificates gcc musl-dev libc-dev

# Cache-friendly download of go dependencies.
ADD go.mod go.sum /app/
WORKDIR /app
RUN go mod download

COPY --from=libjq /libjq /libjq
ADD . /app

# Clone shell-operator to get frameworks
RUN git clone https://github.com/flant/shell-operator shell-operator-clone && \
    cd shell-operator-clone && \
    git checkout v1.0.9

RUN shellOpVer=$(go list -m all | grep shell-operator | cut -d' ' -f 2-) \
    CGO_ENABLED=1 \
    CGO_CFLAGS="-I/libjq/include" \
    CGO_LDFLAGS="-L/libjq/lib" \
    GOOS=linux \
    go build -ldflags="-linkmode external -extldflags '-static' -s -w -X 'github.com/flant/shell-operator/pkg/app.Version=$shellOpVer' -X 'github.com/flant/addon-operator/pkg/app.Version=$appVersion'" \
             -tags='release' \
             -o addon-operator \
             ./cmd/addon-operator

# Final image
FROM --platform=${TARGETPLATFORM:-linux/amd64} alpine:3.15
ARG TARGETPLATFORM
# kubectl url has no variant (v7)
# helm url has dashes and no variant (v7)
RUN apk --no-cache add ca-certificates bash sed tini && \
    kubectlArch=$(echo ${TARGETPLATFORM:-linux/amd64} | sed 's/\/v7//') && \
    echo "Download kubectl for ${kubectlArch}" && \
    wget https://storage.googleapis.com/kubernetes-release/release/v1.21.10/bin/${kubectlArch}/kubectl -O /bin/kubectl && \
    chmod +x /bin/kubectl && \
    helmArch=$(echo ${TARGETPLATFORM:-linux/amd64} | sed 's/\//-/g;s/-v7//') && \
    wget https://get.helm.sh/helm-v3.5.4-${helmArch}.tar.gz -O /helm.tgz && \
    tar -z -x -C /bin -f /helm.tgz --strip-components=1 ${helmArch}/helm && \
    rm -f /helm.tgz

COPY --from=libjq /bin/jq /usr/bin
COPY --from=builder /app/addon-operator /
COPY --from=builder /app/shell-operator-clone/frameworks/shell/ /framework/shell/
COPY --from=builder /app/shell-operator-clone/shell_lib.sh /
WORKDIR /

ENV MODULES_DIR /modules
ENV GLOBAL_HOOKS_DIR /global-hooks
ENTRYPOINT ["/sbin/tini", "--", "/addon-operator"]
