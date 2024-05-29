#
# Builder image
#

FROM restic/restic:0.16.4 AS restic
FROM rclone/rclone:1.66.0 AS rclone

#
# Final image
#
FROM alpine:3.20

RUN apk add --update --no-cache ca-certificates fuse nfs-utils openssh tzdata bash curl docker-cli postgresql-client mariadb-client

ENV RESTIC_REPOSITORY /mnt/restic

COPY --from=restic /usr/bin/restic /usr/local/bin/
COPY --from=rclone /usr/local/bin/rclone /usr/local/bin/

ENTRYPOINT ["/bin/sh", "-c"]

CMD ["while [ ! -f /tmp/done ]; do sleep 1; done"]
