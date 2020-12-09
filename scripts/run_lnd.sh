#!/bin/bash

if [ $# -ne 2 ];
    then echo "lnd image and ID required"
    exit 1
fi

path=$(pwd)
name=lnd_"$2"
mkdir -p "$name"

sudo docker run \
  -d \
  --rm \
  --name "$name" \
  --network regtest \
  -p $((10009 + $2)):10009 \
  -p $((9735 + $2)):9735 \
  -v "$path/$name:/root/.lnd"\
  $1 \
  --rpclisten=0.0.0.0:10009 \
  --listen=0.0.0.0:9735 \
  --bitcoin.active \
  --bitcoin.regtest \
  --bitcoin.node=bitcoind \
  --bitcoind.rpchost=bitcoind \
  --bitcoind.rpcuser=lightning \
  --bitcoind.rpcpass=lightning \
  --bitcoind.zmqpubrawblock=tcp://bitcoind:28332 \
  --bitcoind.zmqpubrawtx=tcp://bitcoind:28333 \
  --debuglevel=debug \
  --externalip="$name" \
  --noseedbackup

source ./scripts/waitnoerr.sh

LNCLI="docker exec "$name" lncli --network regtest"

waitnoerror $LNCLI getinfo
