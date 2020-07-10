package channeldb

import (
	"bytes"
	"errors"

	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightningnetwork/lnd/channeldb/kvdb"
	"github.com/lightningnetwork/lnd/routing/route"
)

var (
	// peerBucket is the name of a top level bucket in which we store
	// information about our peers. Peers are keyed by their pubkey, with
	// data nested per-channel point.
	//
	//
	// peers-bucket
	//      |
	//      |-- <peer-pubkey>
	//      |        |
	//      |        |--<chan-point>
	//     	|        |	|--flap-count-key: <flap count>
	//      |        |--<chan-point>
	//     	|		|--flap-count-key: <flap count>
	//      |
	//      |-- <peer-pubkey>
	//      |        |--<chan-point>
	//     	|		|--flap-count-key: <flap count>
	peerBucket = []byte("peers-bucket")

	// flapCountKey is a key used in the peer pubkey sub-bucket that stores
	// a peer's all time flap count.
	flapCountKey = []byte("flap-count")
)

var (
	// ErrNoPeerBucket is returned when we try to read entries for a peer
	// that is not tracked.
	ErrNoPeerBucket = errors.New("peer bucket not found")

	// ErrNoPeerChanBucket is returned when we try to read entries for a
	// a channel that we have no data tracked for.
	ErrNoPeerChanBucket = errors.New("peer info for channel not found")

	// ErrNoFlapCount is returned when we do not have a flap count stored
	// for a peer.
	ErrNoFlapCount = errors.New("no flap count recorded")
)

// WriteFlapCounts writes the flap count for a set of peers to disk, creating a
// bucket for the peer's pubkey if necessary. Note that this function overwrites
// the current value.
func (d *DB) WriteFlapCounts(flapCounts map[route.Vertex]map[wire.OutPoint]uint32) error {
	return d.Update(func(tx walletdb.ReadWriteTx) error {
		// Run through our set of flap counts and record them for
		// each peer, creating a bucket for the peer pubkey and channel
		// if required.
		for peer, channels := range flapCounts {
			// Run through all the channels we have with this peer,
			// and write their flap count to disk, creating a bucket
			// for the channel if required.
			for channel, count := range channels {
				chanBucket, err := getPeerChannelWriteBucket(
					tx, peer, channel,
				)
				if err != nil {
					return err
				}

				var b bytes.Buffer
				if err := WriteElement(&b, count); err != nil {
					return err
				}

				err = chanBucket.Put(flapCountKey, b.Bytes())
				if err != nil {
					return err
				}
			}
		}

		return nil
	})
}

// getPeerChannelWriteBucket returns a write bucket for a peer and channel
// combination, creating nested buckets if required.
func getPeerChannelWriteBucket(tx kvdb.RwTx, peer route.Vertex,
	channel wire.OutPoint) (kvdb.RwBucket, error) {

	peers := tx.ReadWriteBucket(peerBucket)

	peerBucket, err := peers.CreateBucketIfNotExists(peer[:])
	if err != nil {
		return nil, err
	}

	var chanBuf bytes.Buffer
	err = WriteElement(&chanBuf, channel)
	if err != nil {
		return nil, err
	}

	return peerBucket.CreateBucketIfNotExists(chanBuf.Bytes())
}

// ReadFlapCount attempts to read the flap count for a peer, failing if the
// peer is not found or we do not have flap count stored.
func (d *DB) ReadFlapCount(pubkey route.Vertex, channel wire.OutPoint) (uint32, error) {
	var flapCount uint32

	if err := d.View(func(tx walletdb.ReadTx) error {
		chanBucket, err := getPeerChannelReadBucket(tx, pubkey, channel)
		if err != nil {
			return err
		}

		flapBytes := chanBucket.Get(flapCountKey)
		if flapBytes == nil {
			return ErrNoFlapCount
		}

		r := bytes.NewReader(flapBytes)
		return ReadElement(r, &flapCount)

	}); err != nil {
		return 0, err
	}

	return flapCount, nil
}

// getPeerChannelReadBucket returns a read bucket for a peer and channel
// combination, failing if the nested buckets do not exist.
func getPeerChannelReadBucket(tx kvdb.RTx, peer route.Vertex,
	channel wire.OutPoint) (kvdb.RBucket, error) {

	peers := tx.ReadBucket(peerBucket)

	peerBucket := peers.NestedReadBucket(peer[:])
	if peerBucket == nil {
		return nil, ErrNoPeerBucket
	}

	var chanbuf bytes.Buffer
	if err := WriteElement(&chanbuf, channel); err != nil {
		return nil, err
	}

	chanBucket := peerBucket.NestedReadBucket(chanbuf.Bytes())
	if chanBucket == nil {
		return nil, ErrNoPeerChanBucket
	}

	return chanBucket, nil
}
