FROM debian:12-slim

RUN apt update && \
    apt upgrade -y && \
    apt install -y gnupg2 apt-utils ca-certificates apt-transport-https wget

RUN wget -O- "https://keys.openpgp.org/vks/v1/by-fingerprint/74FFC6D72B925A34B5D356BDF8A27B36A6E2EAE9" | gpg --dearmor | apt-key add - &&\
	echo "deb [ arch=amd64 signed-by=/etc/apt/keyrings/sogo.asc ] https://packages.sogo.nu/nightly/5/debian/ bookworm bookworm" > /etc/apt/sources.list.d/sogo.list && \
	wget -O- "https://keys.openpgp.org/vks/v1/by-fingerprint/74FFC6D72B925A34B5D356BDF8A27B36A6E2EAE9" | tee /etc/apt/keyrings/sogo.asc && \
	apt update

RUN apt update && \
    # Switch to non-interactive install
    export DEBIAN_FRONTEND=noninteractive && \
    # Do the time zone settings beforehand so we don't get promted for it during install (pick your timezone)
    echo "tzdata tzdata/Areas select America" | debconf-set-selections && \
    echo "tzdata tzdata/Zones/America select New_York" | debconf-set-selections && \
    echo "tzdata tzdata/Zones/Etc select UTC" | debconf-set-selections && \
    # For some reason SOGo wants this file to be present
    mkdir -p /usr/share/doc/sogo/ && touch /usr/share/doc/sogo/foo.sh && \
    # Install the actual services needed
    apt install -y --no-install-recommends apache2 sogo supervisor gosu sope4.9-gdl1-postgresql sogo-activesync libwbxml2-dev && \
    # Prepare non-privileged file permissions for SOGo
    chown sogo /etc/sogo/sogo.conf && \
    mkdir -p /var/run/sogo && chown sogo /var/run/sogo && \
    # Apache also runs on a non-privileged user
    mkdir -p /var/run/apache2 && chown www-data /var/run/apache2 && \
    # Activate the necessary Apache mods and disable the default site
    a2enmod proxy headers proxy_http rewrite && \
    a2dissite 000-default && \
    # Verify that gosu works
    gosu nobody true

# This file is explained below
COPY supervisord.conf /etc/supervisord/supervisord.conf

# Supervisord will start Apache and SOGo
CMD ["/usr/bin/supervisord", "-c", "/etc/supervisord/supervisord.conf"]