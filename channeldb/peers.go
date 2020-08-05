package channeldb

import (
	"bytes"
	"errors"

	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightningnetwork/lnd/routing/route"
)

var (
	// peerBucket is the name of a top level bucket in which we store
	// information about our peers. Information for different peers is
	// stored in buckets keyed by their public key.
	//
	//
	// peers-bucket
	//      |
	//      |-- <peer-pubkey>
	//      |        |--flap-count-key: <flap count>
	//      |
	//      |-- <peer-pubkey>
	//      |        |--flap-count-key: <flap count>
	peerBucket = []byte("peers-bucket")

	// flapCountKey is a key used in the peer pubkey sub-bucket that stores
	// a peer's all time flap count.
	flapCountKey = []byte("flap-count")
)

var (
	// ErrNoPeerBucket is returned when we try to read entries for a peer
	// that is not tracked.
	ErrNoPeerBucket = errors.New("peer bucket not found")

	// ErrNoFlapCount is returned when we do not have a flap count stored
	// for a peer.
	ErrNoFlapCount = errors.New("no flap count recorded")
)

// WriteFlapCounts writes the flap count for a set of peers to disk, creating a
// bucket for the peer's pubkey if necessary. Note that this function overwrites
// the current value.
func (d *DB) WriteFlapCounts(flapCounts map[route.Vertex]uint32) error {
	return d.Update(func(tx walletdb.ReadWriteTx) error {
		// Run through our set of flap counts and record them for
		// each peer, creating a bucket for the peer pubkey if required.
		for peer, flapCount := range flapCounts {
			peers := tx.ReadWriteBucket(peerBucket)

			peerBucket, err := peers.CreateBucketIfNotExists(
				peer[:],
			)
			if err != nil {
				return err
			}

			var b bytes.Buffer
			if err := WriteElement(&b, flapCount); err != nil {
				return err
			}

			err = peerBucket.Put(flapCountKey, b.Bytes())
			if err != nil {
				return err
			}
		}

		return nil
	})
}

// ReadFlapCount attempts to read the flap count for a peer, failing if the
// peer is not found or we do not have flap count stored.
func (d *DB) ReadFlapCount(pubkey route.Vertex) (uint32, error) {
	var flapCount uint32

	if err := d.View(func(tx walletdb.ReadTx) error {
		peers := tx.ReadBucket(peerBucket)

		peerBucket := peers.NestedReadBucket(pubkey[:])
		if peerBucket == nil {
			return ErrNoPeerBucket
		}

		flapBytes := peerBucket.Get(flapCountKey)
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
