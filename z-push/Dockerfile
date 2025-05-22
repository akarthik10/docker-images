FROM debian:12.11-slim
ENV ZPUSH_VERSION=2.7.3

ARG IMAP_SERVER
ARG IMAP_PORT
ARG TIMEZONE
ARG SMTP_SERVER
ARG SMTP_PORT

ENV IMAP_SERVER=${IMAP_SERVER}
ENV IMAP_PORT=${IMAP_PORT}
ENV TIMEZONE=${TIMEZONE}
ENV SMTP_SERVER=${SMTP_SERVER}
ENV SMTP_PORT=${SMTP_PORT}

ARG DEBIAN_FRONTEND=noninteractive
RUN apt update && \
    curl -sSL https://packages.sury.org/php/README.txt | bash -x && \
    apt update && \
    apt -y install supervisor nginx wget php8.2 php8.2-cli php8.2-soap php8.2-mbstring php8.2-imap php8.2-fpm php8.2-intl && \
    mkdir -p /usr/local/lib/z-push/ /var/log/z-push /state && \
    chmod 755 /usr/local/lib/z-push/ /var/log/z-push && \
    chown www-data:www-data /state && \
    wget -O z-push.tar.gz https://github.com/Z-Hub/Z-Push/archive/refs/tags/${ZPUSH_VERSION}.tar.gz && \
    tar xzvf z-push.tar.gz && \
    cp -r Z-Push-${ZPUSH_VERSION}/src/* /usr/local/lib/z-push/ && \
    rm /etc/nginx/sites-enabled/default && \
    rm -rf z-push.tar.gz Z-Push-${ZPUSH_VERSION} && \
    ln -sf /proc/1/fd/1 /var/log/nginx/access.log && \
    ln -sf /proc/1/fd/2 /var/log/nginx/error.log && \
    touch /var/log/z-push/z-push.log && \
    touch /var/log/z-push/z-push-error.log && \
    chown www-data:www-data /var/log/z-push/z-push.log && \
    chown www-data:www-data /var/log/z-push/z-push-error.log && \
    sed -i "/'TIMEZONE'/c\    define('TIMEZONE', 'America/New_York');" /usr/local/lib/z-push/config.php && \
    sed -i "/'BACKEND_PROVIDER'/c\    define('BACKEND_PROVIDER', 'BackendIMAP');" /usr/local/lib/z-push/config.php && \
    sed -i "/'IMAP_OPTIONS'/c\    define('IMAP_OPTIONS', '/ssl');" /usr/local/lib/z-push/backend/imap/config.php

COPY ./supervisord.conf /etc/supervisord.conf
COPY ./nginx.conf /etc/nginx/sites-enabled/default.conf
COPY ./run.sh /run.sh
CMD ["/run.sh"]

EXPOSE 80
