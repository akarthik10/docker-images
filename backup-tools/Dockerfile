#
# Builder image
#

FROM restic/restic:0.17.0 AS restic
FROM rclone/rclone:1.67.0 AS rclone

#
# Final image
#
FROM alpine:3.20

RUN apk add --update --no-cache ca-certificates fuse nfs-utils openssh tzdata bash curl docker-cli postgresql-client mariadb-client

ENV RESTIC_REPOSITORY /mnt/restic

COPY --from=restic /usr/bin/restic /usr/local/bin/
COPY --from=rclone /usr/local/bin/rclone /usr/local/bin/

COPY backup-tools /root

ENV RCLONE_COPYCONFIG_COMMAND "mkdir -p /root/.config/rclone; \
cp /tmp/rclone-config/rclone.conf /root/.config/rclone/rclone.conf; "
ENV RCLONE_ABOUT_COMMAND rclone about 'gdrive-hc-backup:'
ENV RESTIC_BACKUP_COMMAND restic --verbose=1 --host home-cluster backup /backup-data

ENV RESTIC_FORGET_COMMAND restic forget --keep-last 10 --keep-hourly 24 --keep-daily 30 --keep-weekly 8 --keep-monthly 6 --keep-yearly 2 --prune
ENV RESTIC_CHECK_COMMAND restic check

ENV SLEEP_DURATION_POST_BACKUP 60

ENTRYPOINT ["/root/backup-tools"]

# CMD ["while [ ! -f /tmp/done ]; do sleep 1; done"]
