FROM n8nio/n8n:0.224.1-debian

ARG PYTHON_VERSION=3.10
ENV PYENV_ROOT="/root/.pyenv"
ENV PATH="$PYENV_ROOT/shims:$PYENV_ROOT/bin:$PATH"

RUN <<PYTHON_INSTALL
    apt-get update
    apt-get install -y git
    git clone --depth=1 https://github.com/pyenv/pyenv.git /root/.pyenv
    pyenv install $PYTHON_VERSION
    pyenv global $PYTHON_VERSION
PYTHON_INSTALL

COPY requirements.txt /tmp/

RUN <<EOF
    apt-get install -y python3-pip
    pip3 install --upgrade pip
    pip3 install --requirement /tmp/requirements.txt
    apt-get autoremove -y
    apt-get clean
    npm config set legacy-peer-deps true
    cd /usr/local/lib/node_modules/n8n
    npm -dd install n8n-nodes-python
EOF

RUN chmod go+x /root
