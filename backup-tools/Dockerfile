#
# Builder image
#
FROM golang:1.16-alpine3.12 AS builder


ARG TARGETPLATFORM
ARG TARGETARCH
ARG TARGETVARIANT

RUN echo "TARGETPLATFORM=$TARGETPLATFORM"
RUN echo "TARGETARCH=$TARGETARCH"
RUN echo "TARGETVARIANT=$TARGETVARIANT"

# Manually strip off "v" on renovate updates, update SHA256 manually
ARG RESTIC_VERSION=0.14.0

# Manually strip off "v" on renovate updates, update SHA256 manually
ARG RCLONE_VERSION=1.59.2

RUN apk add --no-cache curl


RUN curl -sL --fail -o restic.bz2 https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_linux_${TARGETARCH}.bz2 \
 && bzip2 -d -v restic.bz2 \
 && mv restic /usr/local/bin/restic \
 && chmod +x /usr/local/bin/restic

 RUN curl -sL --fail -o rclone.zip https://github.com/rclone/rclone/releases/download/v${RCLONE_VERSION}/rclone-v${RCLONE_VERSION}-linux-${TARGETARCH}.zip \
 && unzip rclone.zip \
 && mv rclone-v${RCLONE_VERSION}-linux-${TARGETARCH}/rclone /usr/local/bin/rclone \
 && chmod +x /usr/local/bin/rclone \
 && rm -rf rclone-v${RCLONE_VERSION}-linux-${TARGETARCH} \
 && rm rclone.zip

#
# Final image
#
FROM alpine:3.17

RUN apk add --update --no-cache ca-certificates fuse nfs-utils openssh tzdata bash curl docker-cli postgresql-client mariadb-client

ENV RESTIC_REPOSITORY /mnt/restic

COPY --from=builder /usr/local/bin/* /usr/local/bin/

ENTRYPOINT ["/bin/sh", "-c"]

CMD ["while [ ! -f /tmp/done ]; do sleep 1; done"]
