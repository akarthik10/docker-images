FROM debian:12-slim AS builder

RUN apt update && \
    export DEBIAN_FRONTEND=noninteractive && \
    apt upgrade -y && \
    apt install -y gnupg2 apt-utils ca-certificates apt-transport-https wget nginx gosu

RUN wget https://packages.sogo.nu/sources/SOPE-5.11.0.tar.gz 
RUN wget https://packages.sogo.nu/sources/SOGo-5.11.0.tar.gz

RUN apt install -y build-essential gnustep-make gnustep-base-runtime libgnustep-base-dev pkg-config libpq-dev libcurl4-openssl-dev  \
    libz-dev libssl-dev libxml2-dev libldap2-dev 

RUN tar -xvf /SOPE-5.11.0.tar.gz && \
    cd /SOPE && \
    ./configure --with-gnustep --enable-debug --disable-strip && \
    make && \
    make install && \
    rm -rf /SOPE-5.11.0.tar.gz /SOPE && \
    ldconfig

RUN apt install -y libsodium-dev libmemcached-dev libwbxml2-dev libzip-dev libytnef-dev

RUN tar -xvf /SOGo-5.11.0.tar.gz && \
    cd /SOGo-5.11.0 && \
    ./configure --enable-debug --disable-strip && \
    make && \
    make install && \
    cd ActiveSync && \
    make && \
    make install && \
    rm -rf /SOGo-5.11.0.tar.gz /SOGo-5.11.0 && \
    echo "/usr/local/lib/sogo" > /etc/ld.so.conf.d/sogo.conf  && \
    ldconfig 

RUN wget https://github.com/memcached/memcached/archive/refs/tags/1.6.29.tar.gz && \
    apt install -y build-essential libevent-dev pkg-config automake autotools-dev && \
    tar -zxf 1.6.29.tar.gz && \
    cd memcached-* && \
    ./autogen.sh && \
    ./configure && \
    make && \
    ls -lah memcached && \
    cp memcached /usr/local/bin/


RUN ldd /usr/local/sbin/sogod | cut -d" " -f3 | xargs tar --dereference -cvf /tmp/libs.tar
RUN tar -rvf /tmp/libs.tar /usr/local/lib/sogo /usr/share/GNUstep /usr/local/lib/GNUstep/ /etc/GNUstep/GNUstep.conf /usr/sbin/gosu /usr/local/sbin/sogod /usr/local/bin/memcached
RUN ldd /usr/local/bin/memcached | cut -d" " -f3 | xargs tar --dereference -rvf /tmp/libs.tar
RUN find / -name libpq.so.5 | xargs tar --dereference -rvf /tmp/libs.tar
RUN find / -name libwbxml2.so.1 | xargs tar --dereference -rvf /tmp/libs.tar

RUN apt install -y rdfind && \
    which rdfind | xargs tar -rvf /tmp/libs.tar

FROM nginx:1.27.1-bookworm AS runner
# COPY --from=builder /tmp/libs.tar /tmp/libs.tar
RUN --mount=type=bind,from=builder,source=/tmp/libs.tar,target=/tmp/libs.tar ls -lah /tmp/libs.tar  && tar -xvf /tmp/libs.tar && \ 
# rm /tmp/libs.tar && \
echo "/usr/local/lib/sogo" > /etc/ld.so.conf.d/sogo.conf && \
ldconfig && \
groupadd --system sogo && \
useradd --system --gid sogo sogo && \
install -o sogo -g sogo -m 755 -d /var/run/sogo /var/spool/sogo /var/log/sogo && \
rdfind -makesymlinks true -makeresultsfile false /usr/lib/
# RUN apt update && apt install -y strace libcurl4
# USER sogo
# RUN /usr/local/sbin/sogod -WOWorkersCount 1 -WOPort 127.0.0.1:13500 -WOPidFile /var/run/sogo/sogo.pid -WOLogFile - -WONoDetach YES
COPY 50-run-sogo.sh /docker-entrypoint.d
COPY 60-start-memcached.sh /docker-entrypoint.d
