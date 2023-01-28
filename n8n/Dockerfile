FROM n8nio/n8n:0.213.0-debian

RUN apt-get update \
	&& apt-get install -y python3-pip \
    && pip3 install fire \
    && pip3 install --requirement /tmp/requirements.txt \
	&& apt-get autoremove -y \
	&& apt-get clean \
    && cd /usr/local/lib/node_modules/n8n \
    && npm -dd install n8n-nodes-python
