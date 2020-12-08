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

function peerCheck(){
if ! $LNCLI_0 listpeers | grep -q $1;
then
   return 1
fi
}

# requiredErrCheck checks whether our peer's logs contain an error due to an
# unknown feature bit. It takes an argument which is true if we expect this
# error.
function requiredErrCheck(){
  requiredErr="unable to start peer: Peer set unknown local feature bits"
  if docker logs lnd_1 | grep -q "$requiredErr";
  then
     echo "feature bit error found, expect err: " "$1"
     # If feature bit found, return our expectErr argument.
     return "$1"
  else
    echo "feature bit error not found, expect err: " "$1"
    # If feature bit found, return !expectedErr
    if $1; then return 0; else return 0;fi;
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

# Check that we have no feature bit problems.
waitnoerror requiredErrCheck 0

echo Opened channel "$txid"

# Next, we upgrade to master (which has no patch in it), to confirm that we
# need this patch at all. Since lnd will continuously connect and disconnect,
# We just check our logs for the error we expect on an unknown feature bit.
echo Upgrading lnd to static remote required without patch
docker stop lnd_0
./scripts/run_lnd.sh carlakirkcohen/lnd:master 0

waitnoerror requiredErrCheck 1

# Now, we upgrade to our patch, check our channel, peer connection and log to
# ensure that we do not have any errors regarding feature bits.
docker stop lnd_0
./scripts/run_lnd.sh carlakirkcohen/lnd:4800 0

waitnoerror chanCheck "$txid"
echo Channel "$txid" still open after upgrade

waitnoerror peerCheck "$PK_1"
echo Connected to peer "$PK_1" after upgrade

waitnoerror requiredErrCheck 0

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
