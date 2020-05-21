package channeldb

import (
	"bytes"
	"errors"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/lightningnetwork/lnd/channeldb/kvdb"
)

var (
	// closeSummaryBucket is a top level bucket which holds additional
	// information about channel closes. It nests channels by chainhash
	// and channel point.
	// [closeSummaryBucket]
	//	[chainHashBucket]
	//		[channelBucket]
	//			[resolversBucket]
	closeSummaryBucket = []byte("close-summaries")

	// resolversBucket holds the outcome of a channel's resolvers. It is
	// nested under a channel and chainhash bucket in the closed channel
	// bucket.
	resolversBucket = []byte("resolvers-bucket")
)

var (
	// ErrNoCloseSummaryBucket is returned if the report bucket has not been
	// created yet.
	ErrNoCloseSummaryBucket = errors.New("no channel close summary bucket " +
		"present")

	// ErrNoChainHashBucket is returned when we have not created a bucket
	// for the current chain hash.
	ErrNoChainHashBucket = errors.New("no chain hash bucket")

	// ErrNoChannelSummaries is returned when a channel is not found in the
	// chain hash bucket.
	ErrNoChannelSummaries = errors.New("channel bucket not found")
)

// ResolverOutcome describes the outcome for the resolver that the contract
// court reached. This state is not necessarily final, htlcs on our own
// commitment are resolved across two resolvers.
type ResolverOutcome uint8

const (
	// ResolverOutcomeUnknown is a placeholder for unknown resolver
	// outcomes.
	ResolverOutcomeUnknown ResolverOutcome = iota

	// ResolverOutcomeAnchorRecovered indicates that we swept our own
	// anchor.
	ResolverOutcomeAnchorRecovered

	// ResolverOutcomeAnchorLost indicates that we did not sweep our anchor,
	// or another party claimed it.
	ResolverOutcomeAnchorLost

	// ResolverOutcomeAnchorUnclaimed indicates that the anchor was left
	// unspent.
	ResolverOutcomeAnchorUnclaimed

	// ResolverOutcomeInvalidIncomingHtlc indicates that we previously
	// locked in an invalid htlc that we now cannot resolve.
	ResolverOutcomeInvalidIncomingHtlc

	// ResolverOutcomeIncomingHtlcSuccessTx indicates that we broadcast the
	// first stage of a two stage htlc claim for an incoming htlc with its
	// preimage.
	ResolverOutcomeIncomingHtlcSuccessTx

	// ResolverOutcomeIncomingHtlcTimedOut indicates that an incoming htlc
	// timed out on chain, so we stopped waiting for the preimage.
	ResolverOutcomeIncomingHtlcTimedOut

	// ResolverOutcomeIncomingHtlcFailed indicates that our peer tried to
	// settle an incoming htlc on chain, but we failed their attempt.
	ResolverOutcomeIncomingHtlcFailed

	// ResolverOutcomeIncomingHtlcClaimed indicates that we claimed an
	// incoming htlc with our preimage on chain.
	ResolverOutcomeIncomingHtlcClaimed

	// ResolverTypeOutgoingHtlcTimeoutTx indicates that we broadcast the
	// first stage of a two stage htlc timeout for an outgoing htlc.
	ResolverTypeOutgoingHtlcTimeoutTx
)

// ResolverReport provides an account of the outcome of a resolver. This differs
// from a ContractReport because it does not necessarily fully resolve the
// contract; each step of two stage htlc resolution is included.
type ResolverReport struct {
	// OutPoint is the outpoint that was resolved.
	OutPoint wire.OutPoint

	// Amount is of the output that was resolved.
	Amount btcutil.Amount

	// ResolverOutcome indicates the type of resolution that occurred.
	ResolverOutcome

	// SpendTxID is the transaction ID of the spending transaction that
	// claimed the outpoint. This may be a sweep transaction, or a first
	// stage success/timeout transaction.
	SpendTxID *chainhash.Hash
}

// PutResolverReport creates and commits a transaction that is used to write a
// resolver report to disk.
func (d *DB) PutResolverReport(tx kvdb.RwTx, chainHash chainhash.Hash,
	channelOutpoint *wire.OutPoint, report *ResolverReport) error {

	putReportFunc := func(tx kvdb.RwTx) error {
		return putReport(tx, chainHash, channelOutpoint, report)
	}

	// If the transaction is nil, we'll create a new one.
	if tx == nil {
		return kvdb.Update(d, putReportFunc)
	}

	// Otherwise, we can write the report to disk using the existing
	// transaction.
	return putReportFunc(tx)
}

// putReport puts a report in the bucket provided, with its outpoint as its key.
func putReport(tx kvdb.RwTx, chainHash chainhash.Hash,
	channelOutpoint *wire.OutPoint, report *ResolverReport) error {

	channelBucket, err := fetchReportWriteBucket(
		tx, chainHash, channelOutpoint,
	)
	if err != nil {
		return err
	}

	// If the resolvers bucket does not exist yet, create it.
	resolvers, err := channelBucket.CreateBucketIfNotExists(
		resolversBucket,
	)
	if err != nil {
		return err
	}

	var outPointBuf bytes.Buffer
	if err := writeOutpoint(&outPointBuf, &report.OutPoint); err != nil {
		return err
	}

	var resolverBuf bytes.Buffer
	_, err = resolverBuf.Write([]byte{byte(report.ResolverOutcome)})
	if err != nil {
		return err
	}

	if err := WriteElements(&resolverBuf, report.Amount); err != nil {
		return err
	}

	// If there is no spend txid present, we write false to indicate that
	// there is no spend transaction. If it is non-nil, we write true and
	// the spend transaction.
	if report.SpendTxID == nil {
		err = WriteElement(&resolverBuf, false)
		if err != nil {
			return err
		}
	} else {
		err = WriteElement(&resolverBuf, true)
		if err != nil {
			return err
		}

		err = WriteElement(&resolverBuf, *report.SpendTxID)
		if err != nil {
			return err
		}
	}

	// We do not yet have fees available for these resolutions, so we write
	// false to disk to indicate that this record does not have fee
	// information. This allows us to start writing fees without needing to
	// do an on the fly migration.
	// TODO(carla): add fees to resolver report and write to disk.
	if err = WriteElement(&resolverBuf, false); err != nil {
		return err
	}

	return resolvers.Put(outPointBuf.Bytes(), resolverBuf.Bytes())
}

// FetchChannelReports fetches the set of reports for a channel.
func (d DB) FetchChannelReports(chainHash chainhash.Hash,
	outPoint *wire.OutPoint) ([]*ResolverReport, error) {

	var reports []*ResolverReport

	if err := kvdb.View(d, func(tx kvdb.ReadTx) error {
		chanBucket, err := fetchReportReadBucket(
			tx, chainHash, outPoint,
		)
		if err != nil {
			return err
		}

		// If there are no resolvers for this channel, we simply
		// return nil, because nothing has been persisted yet.
		resolvers := chanBucket.NestedReadBucket(resolversBucket)
		if resolvers == nil {
			return nil
		}

		// Run through each resolution and add it to our set of
		// resolutions.
		if err := resolvers.ForEach(func(k, v []byte) error {
			var report ResolverReport

			// Read our resolver key into the Report's outpoint
			// from the entry's key.
			r := bytes.NewReader(k)
			err := ReadElement(r, &report.OutPoint)
			if err != nil {
				return err
			}

			// Next, we read the output type from the first byte
			// of the value.
			report.ResolverOutcome = ResolverOutcome(v[0])

			// Read our resolver values from the remainder of the
			// value bytes.
			r = bytes.NewReader(v[1:])

			// Read out our amount, and whether we have a spend tx
			// present on disk.
			var hasSpendTxID bool
			if err := ReadElements(
				r, &report.Amount, &hasSpendTxID,
			); err != nil {
				return err
			}

			// If the report has a spend transaction present, read
			// it.
			if hasSpendTxID {
				var spendTxID chainhash.Hash
				err = ReadElement(r, &spendTxID)
				if err != nil {
					return err
				}
				report.SpendTxID = &spendTxID
			}

			// Finally, we read out a boolean that indicates
			// whether we have our fee recorded on disk. Fees are
			// not currently written to disk, but we read this
			// value out for completeness.
			var haveFee bool
			if err = ReadElement(r, &haveFee); err != nil {
				return err
			}

			reports = append(reports, &report)

			return nil
		}); err != nil {
			return err
		}

		return err
	}); err != nil {
		return nil, err
	}

	return reports, nil
}

// fetchReportWriteBucket returns a write channel bucket within the reports
// top level bucket. If the channel's bucket does not yet exist, it will be
// created.
func fetchReportWriteBucket(tx kvdb.RwTx, chainHash chainhash.Hash,
	outPoint *wire.OutPoint) (kvdb.RwBucket, error) {

	// Get the channel close summary bucket.
	closedBucket, err := tx.CreateTopLevelBucket(closeSummaryBucket)
	if err != nil {
		return nil, err
	}

	// Create the chain hash bucket if it does not exist.
	chainHashBkt, err := closedBucket.CreateBucketIfNotExists(chainHash[:])
	if err != nil {
		return nil, err
	}

	var chanPointBuf bytes.Buffer
	if err := writeOutpoint(&chanPointBuf, outPoint); err != nil {
		return nil, err
	}

	return chainHashBkt.CreateBucketIfNotExists(chanPointBuf.Bytes())
}

// fetchReportReadBucket returns a read channel bucket within the reports
// top level bucket. If any bucket along the way does not exist, it will error.
func fetchReportReadBucket(tx kvdb.ReadTx, chainHash chainhash.Hash,
	outPoint *wire.OutPoint) (kvdb.ReadBucket, error) {

	// First fetch the top level channel close summary bucket.
	closeBucket := tx.ReadBucket(closeSummaryBucket)
	if closeBucket == nil {
		return nil, ErrNoCloseSummaryBucket
	}

	chainHashBucket := closeBucket.NestedReadBucket(chainHash[:])
	if chainHashBucket == nil {
		return nil, ErrNoChainHashBucket
	}

	// With the bucket for the node and chain fetched, we can now go down
	// another level, for the channel itself.
	var chanPointBuf bytes.Buffer
	if err := writeOutpoint(&chanPointBuf, outPoint); err != nil {
		return nil, err
	}
	chanBucket := chainHashBucket.NestedReadBucket(chanPointBuf.Bytes())
	if chanBucket == nil {
		return nil, ErrNoChannelSummaries
	}

	return chanBucket, nil
}
