FROM golang:1.12-alpine3.9
ARG appVersion=latest
RUN apk --no-cache add git ca-certificates
ADD . /addon-operator
WORKDIR /addon-operator
RUN go-build.sh $appVersion

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
COPY --from=0 /addon-operator/addon-operator /
WORKDIR /
ENV MODULES_DIR /modules
ENV GLOBAL_HOOKS_DIR /global-hooks
ENTRYPOINT ["/addon-operator"]
