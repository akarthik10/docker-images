FROM n8nio/n8n:0.214.0-debian
COPY requirements.txt /tmp/
RUN <<EOF
    apt-get update
    apt-get install -y python3-pip
    pip3 install --upgrade pip
    pip3 install fire
    pip3 install --requirement /tmp/requirements.txt
	apt-get autoremove -y
	apt-get clean
	npm config set legacy-peer-deps true
	cd /usr/local/lib/node_modules/n8n
	npm -dd install n8n-nodes-python
EOF
