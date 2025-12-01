#!/bin/bash

function install_go() {
    if /usr/local/go/bin/go version >/dev/null 2>&1; then
      echo "go already installed"
    else
        GOFILE=go1.25.0.linux-amd64.tar.gz
        GOTAR=https://go.dev/dl/${GOFILE}

        wget -P /tmp $GOTAR
        sudo rm -rf /usr/local/go
        sudo tar -C /usr/local -xzf /tmp/${GOFILE}

    fi
    grep '/usr/local/go' ${HOME}/.bashrc|| (echo 'export PATH=$PATH:/usr/local/go/bin' >> ${HOME}/.bashrc)
}

function install_rdma_libs() {
    echo "Installing RDMA libraries..."
    sudo apt-get update
    sudo apt-get install -y build-essential libibverbs-dev rdma-core ibverbs-utils
}

SSH_OPTS="-o StrictHostKeyChecking=no"
SSH="ssh ${SSH_OPTS}"

function host_list() {
    geni-get -a | \
        grep -Po '<host name=\\".*?\"' | \
        sed 's/<host name=\\"\(.*\)\\"/\1/' | \
        sort | \
        uniq
}

host=$(hostname -s)
host_count=$(($(/usr/local/etc/emulab/tmcc hostnames | wc -l)-1))

case $host in
    "node0")
        echo Setting up go server on $host.
        install_go
        install_rdma_libs

        echo Setting up go on $host_count nodes.
        SCRIPT_PATH=$(readlink -f "$0")
        for i in $(seq 1 ${host_count}); do
            ${SSH} node${i} ${SCRIPT_PATH}
        done
    ;;

    *)
        echo Setting up go on $host.
        install_go
        install_rdma_libs
    ;;
esac