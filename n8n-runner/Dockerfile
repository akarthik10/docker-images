FROM n8nio/runners:2.17.0
USER root
RUN cd /opt/runners/task-runner-javascript && pnpm add imapflow mailparser @actual-app/api@26.4.0 --allow-build=better-sqlite3
COPY n8n-task-runners.json /etc/n8n-task-runners.json
USER runner
