FROM n8nio/runners:2.21.2
USER root
RUN cd /opt/runners/task-runner-javascript && pnpm add imapflow mailparser @actual-app/api@26.5.2 --allow-build=better-sqlite3
COPY n8n-task-runners.json /etc/n8n-task-runners.json
USER runner
