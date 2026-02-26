FROM n8nio/n8n:1.123.22

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
RUN sed -i '41i if (process.env.NODE_PATH) {\nconsole.log("Initializing node path");\nprocess.env.NODE_PATH=`/opt/nodejs/node-v22.22.0/lib/node_modules:${process.env.NODE_PATH}`;\nmodule.constructor._initPaths();\nconsole.log("Initializing node path done", process.env.NODE_PATH);\n}' /usr/local/lib/node_modules/n8n/node_modules/@n8n/task-runner/dist/start.js
USER node


