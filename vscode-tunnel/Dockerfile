FROM debian:12-slim

# Read the target arch, set OS to alpine-x64 or alpine-arm64, change vs code download url accordingly
ARG TARGETARCH

RUN if [ "${TARGETARCH}" = "amd64" ]; then \
    export VS_CODE_URL="https://code.visualstudio.com/sha/download?build=stable&os=cli-alpine-x64"; \
    elif [ "${TARGETARCH}" = "arm64" ]; then \
    export VS_CODE_URL="https://code.visualstudio.com/sha/download?build=stable&os=cli-alpine-arm64"; \
    fi && \
    echo ${VS_CODE_URL} && \
    apt-get update && \
    apt-get install -y git curl && \
    apt-get clean && \
    export DEBIAN_FRONTEND=noninteractive && \
    rm -rf /var/lib/apt/lists/* && \
    curl -sL "${VS_CODE_URL}" \
        --output /tmp/vscode-cli.tar.gz && \
    tar -xf /tmp/vscode-cli.tar.gz -C /usr/bin && \
    rm /tmp/vscode-cli.tar.gz

CMD [ "code", "tunnel", "--accept-server-license-terms" ]