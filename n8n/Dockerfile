FROM n8nio/n8n:0.224.1-debian

ARG PYTHON_VERSION=3.10

RUN apt-get update && apt-get install -y git

USER node
ENV PYENV_ROOT="/home/node/.pyenv"
ENV PATH="$PYENV_ROOT/shims:$PYENV_ROOT/bin:$PATH"
RUN <<PYTHON_INSTALL
    git clone --depth=1 https://github.com/pyenv/pyenv.git $PYENV_ROOT
    pyenv install $PYTHON_VERSION
    pyenv global $PYTHON_VERSION
PYTHON_INSTALL

COPY requirements.txt /tmp/

USER root
# RUN apt-get install -y python3-pip && \
#     apt-get remove git && \
#         apt-get autoremove -y && \
#         apt-get clean

RUN <<NPM
	npm config set legacy-peer-deps true
	cd /usr/local/lib/node_modules/n8n
	npm -dd install n8n-nodes-python
NPM   

USER node
RUN <<PIP
    pip3 install --upgrade pip
    pip3 install fire
    pip3 install --requirement /tmp/requirements.txt
PIP