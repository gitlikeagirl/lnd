package channeldb

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/btcsuite/btcwallet/walletdb"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/lightningnetwork/lnd/channeldb/kvdb"
)

var (
	testChainHash = [chainhash.HashSize]byte{
		0x51, 0xb6, 0x37, 0xd8, 0xfc, 0xd2, 0xc6, 0xda,
		0x48, 0x59, 0xe6, 0x96, 0x31, 0x13, 0xa1, 0x17,
		0x2d, 0xe7, 0x93, 0xe4,
	}

	testChanPoint1 = wire.OutPoint{
		Hash: chainhash.Hash{
			0x51, 0xb6, 0x37, 0xd8, 0xfc, 0xd2, 0xc6, 0xda,
			0x48, 0x59, 0xe6, 0x96, 0x31, 0x13, 0xa1, 0x17,
			0x2d, 0xe7, 0x93, 0xe4,
		},
		Index: 1,
	}
)

// TestPersistReport tests the writing and retrieval of a report on disk with
// and without a spend txid.
func TestPersistReport(t *testing.T) {
	tests := []struct {
		name      string
		spendTxID *chainhash.Hash
	}{
		{
			name:      "Non-nil spend txid",
			spendTxID: &testChanPoint1.Hash,
		},
		{
			name:      "Nil spend txid",
			spendTxID: nil,
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			db, cleanup, err := makeTestDB()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer cleanup()

			channelOutpoint := testChanPoint1

			testOutpoint := testChanPoint1
			testOutpoint.Index++

			report := &ResolverReport{
				OutPoint:        testOutpoint,
				Amount:          2,
				ResolverOutcome: ResolverOutcomeUnknown,
				SpendTxID:       test.spendTxID,
			}

			// Write report to disk, and ensure it is identical when
			// it is read.
			err = db.PutResolverReport(
				nil, testChainHash, &channelOutpoint, report,
			)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			reports, err := db.FetchChannelReports(
				testChainHash, &channelOutpoint,
			)
			if err != nil {
				t.Fatalf("could not fetch reports: %v",
					err)
			}

			if len(reports) != 1 {
				t.Fatalf("expected 1 report, got: %v",
					len(reports))
			}
			if !reflect.DeepEqual(report, reports[0]) {
				t.Fatalf("expected: %v, got: %v", report,
					reports[0])
			}
		})
	}
}

// TestFetchChannelReadBucket tests retrieval of the reports bucket for a
// channel, testing that the appropriate error is returned based on the state
// of the existing bucket.
func TestFetchChannelReadBucket(t *testing.T) {
	db, cleanup, err := makeTestDB()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer cleanup()

	channelOutpoint := testChanPoint1

	testOutpoint := testChanPoint1
	testOutpoint.Index++

	// First, we expect to fail because our close summary bucket is not
	// present.
	_, err = db.FetchChannelReports(
		testChainHash, &channelOutpoint,
	)
	if err != ErrNoCloseSummaryBucket {
		t.Fatalf("expected: %v, got %v",
			ErrNoCloseSummaryBucket, err)
	}

	// Create the top level bucket to test the case where  we have an
	// unknown chainhash.
	if err = kvdb.Update(db, func(tx walletdb.ReadWriteTx) error {
		_, err := tx.CreateTopLevelBucket(
			closeSummaryBucket,
		)
		return err
	}); err != nil {
		t.Fatalf("could not create top level bucket: %v", err)
	}

	// If our top level summaries bucket is present, but we do not have a
	// bucket for the chainhash provided, we expect fetch reports to fail.
	_, err = db.FetchChannelReports(
		testChainHash, &channelOutpoint,
	)
	if err != ErrNoChainHashBucket {
		t.Fatalf("expected: %v, got %v",
			ErrNoChainHashBucket, err)
	}

	// Finally we write a report to disk and check that we can fetch it.
	report := &ResolverReport{
		OutPoint:        testOutpoint,
		Amount:          2,
		ResolverOutcome: ResolverOutcomeUnknown,
		SpendTxID:       nil,
	}

	err = db.PutResolverReport(
		nil, testChainHash, &channelOutpoint, report,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now that the channel bucket exists, we expect the channel to be
	// successfully fetched, with no reports.
	reports, err := db.FetchChannelReports(testChainHash, &testChanPoint1)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if len(reports) != 1 {
		t.Fatalf("expected 1 report, got: %v", len(reports))
	}

	if !reflect.DeepEqual(reports[0], report) {
		t.Fatalf("expected: %v, got: %v", reports[0], report)
	}
}

// TestFetchChannelWriteBucket tests the creation of missing buckets when
// retrieving the reports bucket.
func TestFetchChannelWriteBucket(t *testing.T) {
	createReportsBucket := func(tx kvdb.RwTx) (kvdb.RwBucket, error) {
		return tx.CreateTopLevelBucket(closedChannelBucket)
	}

	createChainHashBucket := func(reports kvdb.RwBucket) (kvdb.RwBucket,
		error) {

		return reports.CreateBucketIfNotExists(testChainHash[:])
	}

	createChannelBucket := func(chainHash kvdb.RwBucket) (kvdb.RwBucket,
		error) {

		var chanPointBuf bytes.Buffer
		err := writeOutpoint(&chanPointBuf, &testChanPoint1)
		if err != nil {
			return nil, err
		}
		return chainHash.CreateBucketIfNotExists(chanPointBuf.Bytes())
	}

	tests := []struct {
		name  string
		setup func(tx kvdb.RwTx) error
	}{
		{
			name: "no existing buckets",
			setup: func(tx kvdb.RwTx) error {
				return nil
			},
		},
		{
			name: "reports bucket exists",
			setup: func(tx kvdb.RwTx) error {
				_, err := createReportsBucket(tx)
				return err
			},
		},
		{
			name: "chainhash bucket exists",
			setup: func(tx kvdb.RwTx) error {
				reports, err := createReportsBucket(tx)
				if err != nil {
					return err
				}

				_, err = createChainHashBucket(reports)
				return err
			},
		},
		{
			name: "channel bucket exists",
			setup: func(tx kvdb.RwTx) error {
				reports, err := createReportsBucket(tx)
				if err != nil {
					return err
				}

				chainHash, err := createChainHashBucket(reports)
				if err != nil {
					return err
				}

				_, err = createChannelBucket(chainHash)
				return err
			},
		},
	}

	for _, test := range tests {
		test := test

		t.Run(test.name, func(t *testing.T) {
			db, cleanup, err := makeTestDB()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			defer cleanup()

			if err := kvdb.Update(db, test.setup); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Create a transaction to retrieve the bucket within.
			tx, err := db.BeginReadWriteTx()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if _, err = fetchReportWriteBucket(
				tx, testChainHash, &testChanPoint1,
			); err != nil {
				if err := tx.Rollback(); err != nil {
					t.Fatalf("could not rollback "+
						"tx: %v", err)
				}
				t.Fatalf("unxpected error: %v", err)
			}

			if err := tx.Commit(); err != nil {
				t.Fatalf("unxpected error: %v", err)
			}
		})
	}
}
