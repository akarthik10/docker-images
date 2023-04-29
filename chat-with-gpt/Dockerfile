FROM ghcr.io/cogentapps/chat-with-gpt:release as builder
FROM nvidia/cuda:12.1.1-devel-ubuntu20.04 AS server

WORKDIR /app

# Update the package index and install required dependencies
RUN apt-get update && \
    apt-get install -y \
    curl \
    build-essential \
    libssl-dev \
    libffi-dev \
    python3-dev \
    python3-pip \
    openssl

RUN mkdir /usr/local/nvm
ENV NVM_DIR /usr/local/nvm
ENV NODE_VERSION 19.9.0
RUN curl https://raw.githubusercontent.com/nvm-sh/nvm/v0.39.1/install.sh | bash \
    && . $NVM_DIR/nvm.sh \
    && nvm install $NODE_VERSION \
    && nvm alias default $NODE_VERSION \
    && nvm use default

ENV NODE_PATH $NVM_DIR/v$NODE_VERSION/lib/node_modules
ENV PATH $NVM_DIR/versions/node/v$NODE_VERSION/bin:$PATH    

COPY --from=builder /app ./

ENV PORT 3000
CMD ["npm", "run", "start"]