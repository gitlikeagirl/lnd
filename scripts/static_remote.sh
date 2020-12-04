#!/bin/bash

set -ev

./scripts/run_bitcoin.sh
./scripts/run_lnd.sh carlakirkcohen/lnd:v0.11.1-beta 0
./scripts/run_lnd.sh carlakirkcohen/lnd:v0.7.0-beta 1

function syncCheck(){
  if [[ "$($LNCLI_0 getinfo | jq .synced_to_chain -r)" == false ]]
  then
    return 1
  fi
}

function chanCheck(){
if ! $LNCLI_0 listchannels | grep -q $1;
then
   return 1
fi
}

# TODO(carla): export these from run scripts
BTC_CLI="docker exec -u bitcoin bitcoind bitcoin-cli -regtest -rpcuser=lightning -rpcpassword=lightning $@"
LNCLI_0="docker exec lnd_0 lncli --network regtest $@"
LNCLI_1="docker exec lnd_1 lncli --network regtest $@"

# Fund one of our nodes and get us past regtest segwit activation height.
ADDR=$($LNCLI_0 newaddress p2wkh | jq .address -r)
echo address is $ADDR
$BTC_CLI generatetoaddress 350 "$ADDR"

# Make sure we are synced to chain before we open a channel.
echo Waiting for synced to chain
source ./scripts/waitnoerr.sh

waitnoerror syncCheck

# Create and confirm a chanenl between our nodes.
echo Creating channel while static remote is optional

PK_1=$($LNCLI_1 getinfo | jq .identity_pubkey -r)
txid="$($LNCLI_0 openchannel --node_key "$PK_1" --connect lnd_1:9735 --local_amt 15000000 | jq .funding_txid -r)"
$BTC_CLI generatetoaddress 6 "$ADDR"

waitnoerror chanCheck "$txid"

echo Opened channel "$txid"

echo Upgrading lnd to static remote required

# Now we want to upgrade our node that understands static remote to require it.
# Use a docker container which has our patch included in it.
docker stop lnd_0
./scripts/run_lnd.sh carlakirkcohen/lnd:4800 0

waitnoerror chanCheck "$txid"

echo Channel "$txid" still open after upgrade

# Stop everything + cleanup
echo Stopping all docker containers + cleaning up
docker stop lnd_0 lnd_1 bitcoind

echo "Uploading to file.io..."
sudo zip logs.zip lnd_0/logs/bitcoin/regtest/lnd.log lnd_1/logs/bitcoin/regtest/lnd.log
curl -s -F 'file=@logs.zip' https://file.io | xargs -r0 printf 'logs.tar.gz uploaded to %s, can only be downloaded exactly once\n'

sudo rm -rf lnd_0
sudo rm -rf lnd_1
sudo rm logs.zip

set +ev
