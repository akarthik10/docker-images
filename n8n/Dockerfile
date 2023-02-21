FROM n8nio/n8n:0.216.0-debian

ARG PYTHON_VERSION=3.10
ENV PYENV_ROOT="/root/.pyenv"
ENV PATH="$PYENV_ROOT/shims:$PYENV_ROOT/bin:$PATH"

COPY requirements.txt /tmp/
RUN <<EOF
    apt-get update
    apt-get install git
    git clone --depth=1 https://github.com/pyenv/pyenv.git /root/.pyenv
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

RUN pyenv install $PYTHON_VERSION
RUN pyenv global $PYTHON_VERSION
