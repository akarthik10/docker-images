FROM n8nio/n8n:1.43.1

USER root
COPY requirements.txt /tmp/
RUN <<EOF
    apk add --update --no-cache python3 py3-pip curl yt-dlp
    pip3 install --requirement /tmp/requirements.txt --break-system-packages
    npm -dd install n8n-nodes-python youtube-transcript youtubei.js
EOF
USER node


