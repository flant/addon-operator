FROM golang:1.11-alpine3.9
RUN apk --no-cache add git ca-certificates
ADD . /go/src/github.com/flant/addon-operator
RUN go get -d github.com/flant/addon-operator/...
WORKDIR /go/src/github.com/flant/addon-operator
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o addon-operator ./cmd/addon-operator

FROM ubuntu:18.04
RUN apt-get update && \
    apt-get install -y ca-certificates wget jq && \
    rm -rf /var/lib/apt/lists && \
    wget https://storage.googleapis.com/kubernetes-release/release/v1.13.5/bin/linux/amd64/kubectl -O /bin/kubectl && \
    chmod +x /bin/kubectl && \
    wget https://storage.googleapis.com/kubernetes-helm/helm-v2.12.1-linux-amd64.tar.gz -O /helm.tgz && \
    tar -z -x -C /bin -f /helm.tgz --strip-components=1 linux-amd64/helm && \
    rm -f /helm.tgz && \
    helm init --client-only && \
    mkdir /hooks
COPY --from=0 /go/src/github.com/flant/addon-operator/addon-operator /
WORKDIR /
ENV MODULES_DIR /modules
ENV GLOBAL_HOOKS_DIR /global-hooks
ENTRYPOINT ["/addon-operator"]
#CMD ["start"]
