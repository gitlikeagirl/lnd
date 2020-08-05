package channeldb

import (
	"testing"

	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/stretchr/testify/require"
)

// TestFlapCount tests lookup and writing of flap count to disk.
func TestFlapCount(t *testing.T) {
	db, cleanup, err := MakeTestDB()
	require.NoError(t, err)
	defer cleanup()

	// Try to read flap count for a peer that we have no records for.
	_, err = db.ReadFlapCount(testPub)
	require.Equal(t, ErrNoPeerBucket, err)

	var (
		testPub2              = route.Vertex{2, 2, 2}
		peer1FlapCount uint32 = 20
		peer2FlapCount uint32 = 33
	)

	peers := map[route.Vertex]uint32{
		testPub:  peer1FlapCount,
		testPub2: peer2FlapCount,
	}

	err = db.WriteFlapCounts(peers)
	require.NoError(t, err)

	// Lookup flap count for our first pubkey.
	count, err := db.ReadFlapCount(testPub)
	require.NoError(t, err)
	require.Equal(t, peer1FlapCount, count)

	// Lookup our flap count for the second peer.
	count, err = db.ReadFlapCount(testPub2)
	require.NoError(t, err)
	require.Equal(t, peer2FlapCount, count)
}
