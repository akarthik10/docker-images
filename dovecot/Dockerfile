FROM debian:12.12-slim

ENV container=docker \
    LC_ALL=C
ARG DEBIAN_FRONTEND=noninteractive

#ADD dovecot.gpg /etc/apt/keyrings/dovecot.gpg
#ADD dovecot.list /etc/apt/sources.list.d

RUN apt-get -y update && apt-get -y install \
  tini \
  dovecot-imapd && \
  rm -rf /var/lib/apt/lists && \
  groupadd -g 1000 vmail && \
  useradd -u 1000 -g 1000 vmail -d /srv/vmail && \
  passwd -l vmail && \
  rm -rf /etc/dovecot && \
  mkdir /srv/mail && \
  chown vmail:vmail /srv/mail && \
  mkdir /etc/dovecot 

ADD dovecot.conf /etc/dovecot/dovecot.conf

EXPOSE 143
EXPOSE 993

VOLUME ["/etc/dovecot", "/srv/mail"]
ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/sbin/dovecot", "-F"]
