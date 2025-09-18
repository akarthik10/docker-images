FROM n8nio/n8n:1.112.2

USER root
COPY requirements.txt /tmp/
RUN <<EOF
    apk add --update --no-cache python3 py3-pip curl yt-dlp
    pip3 install --requirement /tmp/requirements.txt --break-system-packages
    npm -dd install youtube-transcript youtubei.js 
    cd /usr/local/lib/node_modules/n8n 
    npm install -g imapflow mailparser @actual-app/api # n8n-nodes-python
    cd -
EOF
USER node


