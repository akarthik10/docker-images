FROM n8nio/n8n:1.123.23

# Copy apk and its deps from Alpine 3.23
COPY --from=alpine:3.23 /sbin/apk /sbin/apk
COPY --from=alpine:3.23 /usr/lib/libapk.so* /usr/lib/

USER root
COPY requirements.txt /tmp/
RUN <<EOF
    cd /usr/local/lib/node_modules/n8n 
    npm install -g imapflow mailparser @actual-app/api
    cd -
EOF
USER node


