package channeldb

import (
	"testing"

	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/routing/route"

	"github.com/stretchr/testify/require"
)

// TestFlapCount tests lookup and writing of flap count to disk.
func TestFlapCount(t *testing.T) {
	db, cleanup, err := makeTestDB()
	require.NoError(t, err)
	defer cleanup()

	// Try to read flap count for a peer that we have no records for.
	_, err = db.ReadFlapCount(testPub, testChanPoint1)
	require.Equal(t, ErrNoPeerBucket, err)

	var flapCount uint32 = 20
	peers := map[route.Vertex]map[wire.OutPoint]uint32{
		testPub: {
			testChanPoint1: 20,
		},
	}

	err = db.WriteFlapCounts(peers)
	require.NoError(t, err)

	count, err := db.ReadFlapCount(testPub, testChanPoint1)
	require.NoError(t, err)
	require.Equal(t, flapCount, count)

	// Finally, we try to read flap count for a peer that has a channel
	// stored, but not the one we're querying.
	chanNotFound := wire.OutPoint{Index: 999}
	_, err = db.ReadFlapCount(testPub, chanNotFound)
	require.Equal(t, ErrNoPeerChanBucket, err)
}
