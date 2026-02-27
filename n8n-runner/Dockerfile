FROM n8nio/runners:2.3.0
USER root
RUN cd /opt/runners/task-runner-javascript && pnpm add imapflow mailparser @actual-app/api --allow-build=better-sqlite3
COPY n8n-task-runners.json /etc/n8n-task-runners.json
USER runner
