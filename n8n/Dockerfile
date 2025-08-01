FROM n8nio/n8n:1.105.2

USER root
COPY requirements.txt /tmp/
RUN <<EOF
    apk add --update --no-cache python3 py3-pip curl yt-dlp
    pip3 install --requirement /tmp/requirements.txt --break-system-packages
    npm -dd install youtube-transcript youtubei.js 
    npm install -g pnpm@latest-10
    pnpm i -g
    cd /usr/local/lib/node_modules/n8n 
    pnpm install n8n-nodes-python imapflow mailparser @actual-app/api
    cd -
EOF
USER node


