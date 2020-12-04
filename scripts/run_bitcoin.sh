#!/bin/bash

set -ev

BITCOIND_VERSION=${BITCOIN_VERSION:-0.18.1}

sudo docker run \
  -d \
  --rm \
  --name bitcoind \
  --network regtest \
  -p 18443:18443 \
  -p 28332:28332 \
  -p 28333:28333 \
  ruimarinho/bitcoin-core:$BITCOIND_VERSION \
  -regtest \
  -printtoconsole \
  -zmqpubrawblock=tcp://0.0.0.0:28332 \
  -zmqpubrawtx=tcp://0.0.0.0:28333 \
  -rpcport=18443 \
  -rpcbind=0.0.0.0 \
  -rpcuser=lightning \
  -rpcpassword=lightning \
  -rpcallowip=172.0.0.0/8

source ./scripts/waitnoerr.sh

BTC_CLI="docker exec -u bitcoin bitcoind bitcoin-cli -regtest -rpcuser=lightning -rpcpassword=lightning"

waitnoerror $BTC_CLI getblockchaininfo
