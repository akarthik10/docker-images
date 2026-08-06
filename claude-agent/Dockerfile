FROM node:22-slim

ARG TARGETARCH

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl ca-certificates git python3 make g++ unzip && \
    curl -fsSL -o /usr/local/bin/kubectl \
      "https://dl.k8s.io/release/$(curl -fsSL https://dl.k8s.io/release/stable.txt)/bin/linux/${TARGETARCH}/kubectl" && \
    chmod +x /usr/local/bin/kubectl && \
    npm install -g imapflow@1.6.5 mailparser@3.9.14 @actual-app/api@26.6.0 --allow-build=better-sqlite3 && \
    apt-get purge -y make g++ && apt-get autoremove -y && apt-get clean && \
    rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash -u 1000 agent
USER agent
WORKDIR /home/agent

RUN curl -fsSL https://claude.ai/install.sh | bash
ENV PATH="/home/agent/.local/bin:${PATH}"

CMD ["claude", "remote-control", "--name", "Home Cluster Agent"]
