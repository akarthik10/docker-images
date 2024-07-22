FROM debian:12.6-slim
ENV ZPUSH_VERSION=2.7.3
ARG DEBIAN_FRONTEND=noninteractive
RUN apt update && \
    curl -sSL https://packages.sury.org/php/README.txt | bash -x && \
    apt update && \
    apt -y install supervisor nginx wget php8.2 php8.2-cli php8.2-soap php8.2-mbstring php8.2-imap php8.2-fpm && \
    mkdir /usr/local/lib/z-push/ /var/log/z-push && \
    chmod 755 /usr/local/lib/z-push/ /var/log/z-push && \
    wget -O z-push.tar.gz https://github.com/Z-Hub/Z-Push/archive/refs/tags/${ZPUSH_VERSION}.tar.gz && \
    tar xzvf z-push.tar.gz && \
    cp -r Z-Push-${ZPUSH_VERSION}/src/* /usr/local/lib/z-push/ && \
    rm /etc/nginx/sites-enabled/default

COPY ./supervisord.conf /etc/supervisord.conf
COPY ./nginx.conf /etc/nginx/sites-enabled/default.conf
CMD ["/usr/bin/supervisord", "-n"]

EXPOSE 80