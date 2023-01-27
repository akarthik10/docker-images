FROM n8nio/n8n:0.199.0-debian

RUN apt-get update \
	&& apt-get install -y python3-pip \
    && pip install fire \
	&& apt-get autoremove -y \
	&& apt-get clean

RUN python3 --version

RUN cd /usr/local/lib/node_modules/n8n && npm install n8n-nodes-python
