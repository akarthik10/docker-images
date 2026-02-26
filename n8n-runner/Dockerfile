FROM n8nio/runners:2.3.0
USER root
ENV NODE_OPTIONS="--max-old-space-size=8192"
RUN cd /opt/runners/task-runner-javascript && pnpm add imapflow mailparser @actual-app/api
COPY n8n-task-runners.json /etc/n8n-task-runners.json
USER runner
