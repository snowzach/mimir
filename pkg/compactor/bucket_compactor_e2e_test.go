// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/thanos-io/thanos/blob/2be2db77/pkg/compact/compact_e2e_test.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Thanos Authors.

package compactor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/grafana/dskit/runutil"
	"github.com/oklog/ulid"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/thanos/pkg/objstore/filesystem"
	"golang.org/x/sync/errgroup"

	"github.com/thanos-io/thanos/pkg/block"
	"github.com/thanos-io/thanos/pkg/block/metadata"
	"github.com/thanos-io/thanos/pkg/objstore"
)

const fetcherConcurrency = 32

func TestSyncer_GarbageCollect_e2e(t *testing.T) {
	foreachStore(t, func(t *testing.T, bkt objstore.Bucket) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Generate 10 source block metas and construct higher level blocks
		// that are higher compactions of them.
		var metas []*metadata.Meta
		var ids []ulid.ULID

		for i := 0; i < 10; i++ {
			var m metadata.Meta

			m.Version = 1
			m.ULID = ulid.MustNew(uint64(i), nil)
			m.Compaction.Sources = []ulid.ULID{m.ULID}
			m.Compaction.Level = 1

			ids = append(ids, m.ULID)
			metas = append(metas, &m)
		}

		var m1 metadata.Meta
		m1.Version = 1
		m1.ULID = ulid.MustNew(100, nil)
		m1.Compaction.Level = 2
		m1.Compaction.Sources = ids[:4]
		m1.Thanos.Downsample.Resolution = 0

		var m2 metadata.Meta
		m2.Version = 1
		m2.ULID = ulid.MustNew(200, nil)
		m2.Compaction.Level = 2
		m2.Compaction.Sources = ids[4:8] // last two source IDs is not part of a level 2 block.
		m2.Thanos.Downsample.Resolution = 0

		var m3 metadata.Meta
		m3.Version = 1
		m3.ULID = ulid.MustNew(300, nil)
		m3.Compaction.Level = 3
		m3.Compaction.Sources = ids[:9] // last source ID is not part of level 3 block.
		m3.Thanos.Downsample.Resolution = 0

		var m4 metadata.Meta
		m4.Version = 1
		m4.ULID = ulid.MustNew(400, nil)
		m4.Compaction.Level = 2
		m4.Compaction.Sources = ids[9:] // covers the last block but is a different resolution. Must not trigger deletion.
		m4.Thanos.Downsample.Resolution = 1000

		// Create all blocks in the bucket.
		for _, m := range append(metas, &m1, &m2, &m3, &m4) {
			fmt.Println("create", m.ULID)
			var buf bytes.Buffer
			require.NoError(t, json.NewEncoder(&buf).Encode(&m))
			require.NoError(t, bkt.Upload(ctx, path.Join(m.ULID.String(), metadata.MetaFilename), &buf))
		}

		duplicateBlocksFilter := NewShardAwareDeduplicateFilter()
		metaFetcher, err := block.NewMetaFetcher(nil, 32, objstore.WithNoopInstr(bkt), "", nil, []block.MetadataFilter{
			duplicateBlocksFilter,
		}, nil)
		require.NoError(t, err)

		blocksMarkedForDeletion := promauto.With(nil).NewCounter(prometheus.CounterOpts{})
		garbageCollectedBlocks := promauto.With(nil).NewCounter(prometheus.CounterOpts{})
		ignoreDeletionMarkFilter := block.NewIgnoreDeletionMarkFilter(nil, nil, 48*time.Hour, fetcherConcurrency)
		sy, err := NewMetaSyncer(nil, nil, bkt, metaFetcher, duplicateBlocksFilter, ignoreDeletionMarkFilter, blocksMarkedForDeletion, garbageCollectedBlocks, 1)
		require.NoError(t, err)

		// Do one initial synchronization with the bucket.
		require.NoError(t, sy.SyncMetas(ctx))
		require.NoError(t, sy.GarbageCollect(ctx))

		var rem []ulid.ULID
		err = bkt.Iter(ctx, "", func(n string) error {
			id := ulid.MustParse(n[:len(n)-1])
			deletionMarkFile := path.Join(id.String(), metadata.DeletionMarkFilename)

			exists, err := bkt.Exists(ctx, deletionMarkFile)
			if err != nil {
				return err
			}
			if !exists {
				rem = append(rem, id)
			}
			return nil
		})
		require.NoError(t, err)

		sort.Slice(rem, func(i, j int) bool {
			return rem[i].Compare(rem[j]) < 0
		})

		// Only the level 3 block, the last source block in both resolutions should be left.
		assert.Equal(t, []ulid.ULID{metas[9].ULID, m3.ULID, m4.ULID}, rem)

		// After another sync the changes should also be reflected in the local groups.
		require.NoError(t, sy.SyncMetas(ctx))
		require.NoError(t, sy.GarbageCollect(ctx))

		// Only the level 3 block, the last source block in both resolutions should be left.
		grouper := NewDefaultGrouper("user-1", metadata.NoneFunc)
		groups, err := grouper.Groups(sy.Metas())
		require.NoError(t, err)

		assert.Equal(t, "0@17241709254077376921", groups[0].Key())
		assert.Equal(t, []ulid.ULID{metas[9].ULID, m3.ULID}, groups[0].IDs())
		assert.Equal(t, "1000@17241709254077376921", groups[1].Key())
		assert.Equal(t, []ulid.ULID{m4.ULID}, groups[1].IDs())
	})
}

func TestGroupCompactE2E(t *testing.T) {
	foreachStore(t, func(t *testing.T, bkt objstore.Bucket) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Create fresh, empty directory for actual test.
		dir := t.TempDir()

		// Start dir checker... we make sure that "dir" only contains group subdirectories during compaction,
		// and not any block directories. Dir checker stops when context is canceled, or on first error,
		// in which case error is logger and test is failed. (We cannot use Fatal or FailNow from a goroutine).
		go func() {
			for ctx.Err() == nil {
				fs, err := ioutil.ReadDir(dir)
				if err != nil && !os.IsNotExist(err) {
					t.Log("error while listing directory", dir)
					t.Fail()
					return
				}

				for _, fi := range fs {
					// Suffix used by Prometheus LeveledCompactor when doing compaction.
					toCheck := strings.TrimSuffix(fi.Name(), ".tmp-for-creation")

					_, err := ulid.Parse(toCheck)
					if err == nil {
						t.Log("found block directory in main compaction directory", fi.Name())
						t.Fail()
						return
					}
				}

				select {
				case <-time.After(100 * time.Millisecond):
					continue
				case <-ctx.Done():
					return
				}
			}
		}()

		logger := log.NewLogfmtLogger(os.Stderr)

		reg := prometheus.NewRegistry()

		ignoreDeletionMarkFilter := block.NewIgnoreDeletionMarkFilter(logger, objstore.WithNoopInstr(bkt), 48*time.Hour, fetcherConcurrency)
		duplicateBlocksFilter := NewShardAwareDeduplicateFilter()
		noCompactMarkerFilter := NewGatherNoCompactionMarkFilter(logger, objstore.WithNoopInstr(bkt), 2)
		metaFetcher, err := block.NewMetaFetcher(nil, 32, objstore.WithNoopInstr(bkt), "", nil, []block.MetadataFilter{
			ignoreDeletionMarkFilter,
			duplicateBlocksFilter,
			noCompactMarkerFilter,
		}, nil)
		require.NoError(t, err)

		blocksMarkedForDeletion := promauto.With(nil).NewCounter(prometheus.CounterOpts{})
		garbageCollectedBlocks := promauto.With(nil).NewCounter(prometheus.CounterOpts{})
		sy, err := NewMetaSyncer(nil, nil, bkt, metaFetcher, duplicateBlocksFilter, ignoreDeletionMarkFilter, blocksMarkedForDeletion, garbageCollectedBlocks, 5)
		require.NoError(t, err)

		comp, err := tsdb.NewLeveledCompactor(ctx, reg, logger, []int64{1000, 3000}, nil, nil)
		require.NoError(t, err)

		planner := NewPlanner(logger, []int64{1000, 3000}, noCompactMarkerFilter)
		grouper := NewDefaultGrouper("user-1", metadata.NoneFunc)
		metrics := NewBucketCompactorMetrics(blocksMarkedForDeletion, garbageCollectedBlocks, prometheus.NewPedanticRegistry())
		bComp, err := NewBucketCompactor(logger, sy, grouper, planner, comp, dir, bkt, 2, true, ownAllJobs, sortJobsByNewestBlocksFirst, metrics)
		require.NoError(t, err)

		// Compaction on empty should not fail.
		require.NoError(t, bComp.Compact(ctx))
		assert.Equal(t, 0.0, promtest.ToFloat64(sy.metrics.garbageCollectedBlocks))
		assert.Equal(t, 0.0, promtest.ToFloat64(sy.metrics.blocksMarkedForDeletion))
		assert.Equal(t, 0.0, promtest.ToFloat64(sy.metrics.garbageCollectionFailures))
		assert.Equal(t, 0.0, promtest.ToFloat64(metrics.blocksMarkedForNoCompact))
		assert.Equal(t, 0.0, promtest.ToFloat64(metrics.groupCompactions))
		assert.Equal(t, 0.0, promtest.ToFloat64(metrics.groupCompactionRunsStarted))
		assert.Equal(t, 0.0, promtest.ToFloat64(metrics.groupCompactionRunsCompleted))
		assert.Equal(t, 0.0, promtest.ToFloat64(metrics.groupCompactionRunsFailed))

		_, err = os.Stat(dir)
		assert.True(t, os.IsNotExist(err), "dir %s should be remove after compaction.", dir)

		// Test label name with slash, regression: https://github.com/thanos-io/thanos/issues/1661.
		extLabels := labels.Labels{{Name: "e1", Value: "1/weird"}}
		extLabels2 := labels.Labels{{Name: "e1", Value: "1"}}
		metas := createAndUpload(t, bkt, []blockgenSpec{
			{
				numSamples: 100, mint: 500, maxt: 1000, extLset: extLabels, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "1"}},
					{{Name: "a", Value: "2"}, {Name: "b", Value: "2"}},
					{{Name: "a", Value: "3"}},
					{{Name: "a", Value: "4"}},
				},
			},
			{
				numSamples: 100, mint: 2000, maxt: 3000, extLset: extLabels, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "3"}},
					{{Name: "a", Value: "4"}},
					{{Name: "a", Value: "5"}},
					{{Name: "a", Value: "6"}},
				},
			},
			// Mix order to make sure compactor is able to deduct min time / max time.
			// Currently TSDB does not produces empty blocks (see: https://github.com/prometheus/tsdb/pull/374). However before v2.7.0 it was
			// so we still want to mimick this case as close as possible.
			{
				mint: 1000, maxt: 2000, extLset: extLabels, res: 124,
				// Empty block.
			},
			// Due to TSDB compaction delay (not compacting fresh block), we need one more block to be pushed to trigger compaction.
			{
				numSamples: 100, mint: 3000, maxt: 4000, extLset: extLabels, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "7"}},
				},
			},
			// Extra block for "distraction" for different resolution and one for different labels.
			{
				numSamples: 100, mint: 5000, maxt: 6000, extLset: labels.Labels{{Name: "e1", Value: "2"}}, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "7"}},
				},
			},
			// Extra block for "distraction" for different resolution and one for different labels.
			{
				numSamples: 100, mint: 4000, maxt: 5000, extLset: extLabels, res: 0,
				series: []labels.Labels{
					{{Name: "a", Value: "7"}},
				},
			},
			// Second group (extLabels2).
			{
				numSamples: 100, mint: 2000, maxt: 3000, extLset: extLabels2, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "3"}},
					{{Name: "a", Value: "4"}},
					{{Name: "a", Value: "6"}},
				},
			},
			{
				numSamples: 100, mint: 0, maxt: 1000, extLset: extLabels2, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "1"}},
					{{Name: "a", Value: "2"}, {Name: "b", Value: "2"}},
					{{Name: "a", Value: "3"}},
					{{Name: "a", Value: "4"}},
				},
			},
			// Due to TSDB compaction delay (not compacting fresh block), we need one more block to be pushed to trigger compaction.
			{
				numSamples: 100, mint: 3000, maxt: 4000, extLset: extLabels2, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "7"}},
				},
			},
		}, []blockgenSpec{
			{
				numSamples: 100, mint: 0, maxt: 499, extLset: extLabels, res: 124,
				series: []labels.Labels{
					{{Name: "a", Value: "1"}},
					{{Name: "a", Value: "2"}, {Name: "b", Value: "2"}},
					{{Name: "a", Value: "3"}},
					{{Name: "a", Value: "4"}},
				},
			},
		})

		require.NoError(t, bComp.Compact(ctx))
		assert.Equal(t, 5.0, promtest.ToFloat64(sy.metrics.garbageCollectedBlocks))
		assert.Equal(t, 5.0, promtest.ToFloat64(sy.metrics.blocksMarkedForDeletion))
		assert.Equal(t, 1.0, promtest.ToFloat64(metrics.blocksMarkedForNoCompact))
		assert.Equal(t, 0.0, promtest.ToFloat64(sy.metrics.garbageCollectionFailures))
		assert.Equal(t, 2.0, promtest.ToFloat64(metrics.groupCompactions))
		assert.Equal(t, 12.0, promtest.ToFloat64(metrics.groupCompactionRunsStarted))
		assert.Equal(t, 11.0, promtest.ToFloat64(metrics.groupCompactionRunsCompleted))
		assert.Equal(t, 1.0, promtest.ToFloat64(metrics.groupCompactionRunsFailed))

		_, err = os.Stat(dir)
		assert.True(t, os.IsNotExist(err), "dir %s should be remove after compaction.", dir)

		// Check object storage. All blocks that were included in new compacted one should be removed. New compacted ones
		// are present and looks as expected.
		nonCompactedExpected := map[ulid.ULID]bool{
			metas[3].ULID: false,
			metas[4].ULID: false,
			metas[5].ULID: false,
			metas[8].ULID: false,
			metas[9].ULID: false,
		}
		others := map[string]metadata.Meta{}
		require.NoError(t, bkt.Iter(ctx, "", func(n string) error {
			id, ok := block.IsBlockDir(n)
			if !ok {
				return nil
			}

			if _, ok := nonCompactedExpected[id]; ok {
				nonCompactedExpected[id] = true
				return nil
			}

			meta, err := block.DownloadMeta(ctx, logger, bkt, id)
			if err != nil {
				return err
			}

			others[DefaultGroupKey(meta.Thanos)] = meta
			return nil
		}))

		for id, found := range nonCompactedExpected {
			assert.True(t, found, "not found expected block %s", id.String())
		}

		// We expect two compacted blocks only outside of what we expected in `nonCompactedExpected`.
		assert.Equal(t, 2, len(others))
		{
			meta, ok := others[defaultGroupKey(124, extLabels)]
			assert.True(t, ok, "meta not found")

			assert.Equal(t, int64(500), meta.MinTime)
			assert.Equal(t, int64(3000), meta.MaxTime)
			assert.Equal(t, uint64(6), meta.Stats.NumSeries)
			assert.Equal(t, uint64(2*4*100), meta.Stats.NumSamples) // Only 2 times 4*100 because one block was empty.
			assert.Equal(t, 2, meta.Compaction.Level)
			assert.Equal(t, []ulid.ULID{metas[0].ULID, metas[1].ULID, metas[2].ULID}, meta.Compaction.Sources)

			// Check thanos meta.
			assert.True(t, labels.Equal(extLabels, labels.FromMap(meta.Thanos.Labels)), "ext labels does not match")
			assert.Equal(t, int64(124), meta.Thanos.Downsample.Resolution)
			assert.True(t, len(meta.Thanos.SegmentFiles) > 0, "compacted blocks have segment files set")
		}
		{
			meta, ok := others[defaultGroupKey(124, extLabels2)]
			assert.True(t, ok, "meta not found")

			assert.Equal(t, int64(0), meta.MinTime)
			assert.Equal(t, int64(3000), meta.MaxTime)
			assert.Equal(t, uint64(5), meta.Stats.NumSeries)
			assert.Equal(t, uint64(2*4*100-100), meta.Stats.NumSamples)
			assert.Equal(t, 2, meta.Compaction.Level)
			assert.Equal(t, []ulid.ULID{metas[6].ULID, metas[7].ULID}, meta.Compaction.Sources)

			// Check thanos meta.
			assert.True(t, labels.Equal(extLabels2, labels.FromMap(meta.Thanos.Labels)), "ext labels does not match")
			assert.Equal(t, int64(124), meta.Thanos.Downsample.Resolution)
			assert.True(t, len(meta.Thanos.SegmentFiles) > 0, "compacted blocks have segment files set")
		}
	})
}

type blockgenSpec struct {
	mint, maxt int64
	series     []labels.Labels
	numSamples int
	extLset    labels.Labels
	res        int64
}

func createAndUpload(t testing.TB, bkt objstore.Bucket, blocks []blockgenSpec, blocksWithOutOfOrderChunks []blockgenSpec) (metas []*metadata.Meta) {
	prepareDir, err := ioutil.TempDir("", "test-compact-prepare")
	require.NoError(t, err)
	defer func() { require.NoError(t, os.RemoveAll(prepareDir)) }()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for _, b := range blocks {
		id, meta := createBlock(ctx, t, prepareDir, b)
		metas = append(metas, meta)
		require.NoError(t, block.Upload(ctx, log.NewNopLogger(), bkt, filepath.Join(prepareDir, id.String()), metadata.NoneFunc))
	}
	for _, b := range blocksWithOutOfOrderChunks {
		id, meta := createBlock(ctx, t, prepareDir, b)

		err := putOutOfOrderIndex(filepath.Join(prepareDir, id.String()), b.mint, b.maxt)
		require.NoError(t, err)

		metas = append(metas, meta)
		require.NoError(t, block.Upload(ctx, log.NewNopLogger(), bkt, filepath.Join(prepareDir, id.String()), metadata.NoneFunc))
	}

	return metas
}

func createBlock(ctx context.Context, t testing.TB, prepareDir string, b blockgenSpec) (id ulid.ULID, meta *metadata.Meta) {
	var err error
	if b.numSamples == 0 {
		id, err = createEmptyBlock(prepareDir, b.mint, b.maxt, b.extLset, b.res)
	} else {
		id, err = createBlockWithOptions(ctx, prepareDir, b.series, b.numSamples, b.mint, b.maxt, b.extLset, b.res, false, metadata.NoneFunc)
	}
	require.NoError(t, err)

	meta, err = metadata.ReadFromDir(filepath.Join(prepareDir, id.String()))
	require.NoError(t, err)
	return
}

// Regression test for #2459 issue.
func TestGarbageCollectDoesntCreateEmptyBlocksWithDeletionMarksOnly(t *testing.T) {
	logger := log.NewLogfmtLogger(os.Stderr)

	foreachStore(t, func(t *testing.T, bkt objstore.Bucket) {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Generate two blocks, and then another block that covers both of them.
		var metas []*metadata.Meta
		var ids []ulid.ULID

		for i := 0; i < 2; i++ {
			var m metadata.Meta

			m.Version = 1
			m.ULID = ulid.MustNew(uint64(i), nil)
			m.Compaction.Sources = []ulid.ULID{m.ULID}
			m.Compaction.Level = 1

			ids = append(ids, m.ULID)
			metas = append(metas, &m)
		}

		var m1 metadata.Meta
		m1.Version = 1
		m1.ULID = ulid.MustNew(100, nil)
		m1.Compaction.Level = 2
		m1.Compaction.Sources = ids
		m1.Thanos.Downsample.Resolution = 0

		// Create all blocks in the bucket.
		for _, m := range append(metas, &m1) {
			fmt.Println("create", m.ULID)
			var buf bytes.Buffer
			require.NoError(t, json.NewEncoder(&buf).Encode(&m))
			require.NoError(t, bkt.Upload(ctx, path.Join(m.ULID.String(), metadata.MetaFilename), &buf))
		}

		blocksMarkedForDeletion := promauto.With(nil).NewCounter(prometheus.CounterOpts{})
		garbageCollectedBlocks := promauto.With(nil).NewCounter(prometheus.CounterOpts{})
		ignoreDeletionMarkFilter := block.NewIgnoreDeletionMarkFilter(nil, objstore.WithNoopInstr(bkt), 48*time.Hour, fetcherConcurrency)

		duplicateBlocksFilter := NewShardAwareDeduplicateFilter()
		metaFetcher, err := block.NewMetaFetcher(nil, 32, objstore.WithNoopInstr(bkt), "", nil, []block.MetadataFilter{
			ignoreDeletionMarkFilter,
			duplicateBlocksFilter,
		}, nil)
		require.NoError(t, err)

		sy, err := NewMetaSyncer(nil, nil, bkt, metaFetcher, duplicateBlocksFilter, ignoreDeletionMarkFilter, blocksMarkedForDeletion, garbageCollectedBlocks, 1)
		require.NoError(t, err)

		// Do one initial synchronization with the bucket.
		require.NoError(t, sy.SyncMetas(ctx))
		require.NoError(t, sy.GarbageCollect(ctx))
		assert.Equal(t, 2.0, promtest.ToFloat64(garbageCollectedBlocks))

		rem, err := listBlocksMarkedForDeletion(ctx, bkt)
		require.NoError(t, err)

		sort.Slice(rem, func(i, j int) bool {
			return rem[i].Compare(rem[j]) < 0
		})

		assert.Equal(t, ids, rem)

		// Delete source blocks.
		for _, id := range ids {
			require.NoError(t, block.Delete(ctx, logger, bkt, id))
		}

		// After another garbage-collect, we should not find new blocks that are deleted with new deletion mark files.
		require.NoError(t, sy.SyncMetas(ctx))
		require.NoError(t, sy.GarbageCollect(ctx))

		rem, err = listBlocksMarkedForDeletion(ctx, bkt)
		require.NoError(t, err)
		assert.Equal(t, 0, len(rem))
	})
}

func listBlocksMarkedForDeletion(ctx context.Context, bkt objstore.Bucket) ([]ulid.ULID, error) {
	var rem []ulid.ULID
	err := bkt.Iter(ctx, "", func(n string) error {
		id := ulid.MustParse(n[:len(n)-1])
		deletionMarkFile := path.Join(id.String(), metadata.DeletionMarkFilename)

		exists, err := bkt.Exists(ctx, deletionMarkFile)
		if err != nil {
			return err
		}
		if exists {
			rem = append(rem, id)
		}
		return nil
	})
	return rem, err
}

func foreachStore(t *testing.T, testFn func(t *testing.T, bkt objstore.Bucket)) {
	t.Parallel()

	// Mandatory Inmem. Not parallel, to detect problem early.
	if ok := t.Run("inmem", func(t *testing.T) {
		testFn(t, objstore.NewInMemBucket())
	}); !ok {
		return
	}

	// Mandatory Filesystem.
	t.Run("filesystem", func(t *testing.T) {
		t.Parallel()

		dir, err := ioutil.TempDir("", "filesystem-foreach-store-test")
		require.NoError(t, err)
		defer require.NoError(t, os.RemoveAll(dir))

		b, err := filesystem.NewBucket(dir)
		require.NoError(t, err)
		testFn(t, b)
	})
}

// createEmptyBlock produces empty block like it was the case before fix: https://github.com/prometheus/tsdb/pull/374.
// (Prometheus pre v2.7.0).
func createEmptyBlock(dir string, mint, maxt int64, extLset labels.Labels, resolution int64) (ulid.ULID, error) {
	entropy := rand.New(rand.NewSource(time.Now().UnixNano()))
	uid := ulid.MustNew(ulid.Now(), entropy)

	if err := os.Mkdir(path.Join(dir, uid.String()), os.ModePerm); err != nil {
		return ulid.ULID{}, errors.Wrap(err, "close index")
	}

	if err := os.Mkdir(path.Join(dir, uid.String(), "chunks"), os.ModePerm); err != nil {
		return ulid.ULID{}, errors.Wrap(err, "close index")
	}

	w, err := index.NewWriter(context.Background(), path.Join(dir, uid.String(), "index"))
	if err != nil {
		return ulid.ULID{}, errors.Wrap(err, "new index")
	}

	if err := w.Close(); err != nil {
		return ulid.ULID{}, errors.Wrap(err, "close index")
	}

	m := tsdb.BlockMeta{
		Version: 1,
		ULID:    uid,
		MinTime: mint,
		MaxTime: maxt,
		Compaction: tsdb.BlockMetaCompaction{
			Level:   1,
			Sources: []ulid.ULID{uid},
		},
	}
	b, err := json.Marshal(&m)
	if err != nil {
		return ulid.ULID{}, err
	}

	if err := ioutil.WriteFile(path.Join(dir, uid.String(), "meta.json"), b, os.ModePerm); err != nil {
		return ulid.ULID{}, errors.Wrap(err, "saving meta.json")
	}

	if _, err = metadata.InjectThanos(log.NewNopLogger(), filepath.Join(dir, uid.String()), metadata.Thanos{
		Labels:     extLset.Map(),
		Downsample: metadata.ThanosDownsample{Resolution: resolution},
		Source:     metadata.TestSource,
	}, nil); err != nil {
		return ulid.ULID{}, errors.Wrap(err, "finalize block")
	}

	return uid, nil
}

func createBlockWithOptions(
	ctx context.Context,
	dir string,
	series []labels.Labels,
	numSamples int,
	mint, maxt int64,
	extLset labels.Labels,
	resolution int64,
	tombstones bool,
	hashFunc metadata.HashFunc,
) (id ulid.ULID, err error) {
	headOpts := tsdb.DefaultHeadOptions()
	headOpts.ChunkDirRoot = filepath.Join(dir, "chunks")
	headOpts.ChunkRange = 10000000000
	h, err := tsdb.NewHead(nil, nil, nil, headOpts, nil)
	if err != nil {
		return id, errors.Wrap(err, "create head block")
	}
	defer func() {
		runutil.CloseWithErrCapture(&err, h, "TSDB Head")
		if e := os.RemoveAll(headOpts.ChunkDirRoot); e != nil {
			err = errors.Wrap(e, "delete chunks dir")
		}
	}()

	var g errgroup.Group
	var timeStepSize = (maxt - mint) / int64(numSamples+1)
	var batchSize = len(series) / runtime.GOMAXPROCS(0)

	for len(series) > 0 {
		l := batchSize
		if len(series) < 1000 {
			l = len(series)
		}
		batch := series[:l]
		series = series[l:]

		g.Go(func() error {
			t := mint

			for i := 0; i < numSamples; i++ {
				app := h.Appender(ctx)

				for _, lset := range batch {
					_, err := app.Append(0, lset, t, rand.Float64())
					if err != nil {
						if rerr := app.Rollback(); rerr != nil {
							err = errors.Wrapf(err, "rollback failed: %v", rerr)
						}

						return errors.Wrap(err, "add sample")
					}
				}
				if err := app.Commit(); err != nil {
					return errors.Wrap(err, "commit")
				}
				t += timeStepSize
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return id, err
	}
	c, err := tsdb.NewLeveledCompactor(ctx, nil, log.NewNopLogger(), []int64{maxt - mint}, nil, nil)
	if err != nil {
		return id, errors.Wrap(err, "create compactor")
	}

	id, err = c.Write(dir, h, mint, maxt, nil)
	if err != nil {
		return id, errors.Wrap(err, "write block")
	}

	if id.Compare(ulid.ULID{}) == 0 {
		return id, errors.Errorf("nothing to write, asked for %d samples", numSamples)
	}

	blockDir := filepath.Join(dir, id.String())

	files := []metadata.File{}
	if hashFunc != metadata.NoneFunc {
		paths := []string{}
		if err := filepath.Walk(blockDir, func(path string, info os.FileInfo, err error) error {
			if info.IsDir() {
				return nil
			}
			paths = append(paths, path)
			return nil
		}); err != nil {
			return id, errors.Wrapf(err, "walking %s", dir)
		}

		for _, p := range paths {
			pHash, err := metadata.CalculateHash(p, metadata.SHA256Func, log.NewNopLogger())
			if err != nil {
				return id, errors.Wrapf(err, "calculating hash of %s", blockDir+p)
			}
			files = append(files, metadata.File{
				RelPath: strings.TrimPrefix(p, blockDir+"/"),
				Hash:    &pHash,
			})
		}
	}

	if _, err = metadata.InjectThanos(log.NewNopLogger(), blockDir, metadata.Thanos{
		Labels:     extLset.Map(),
		Downsample: metadata.ThanosDownsample{Resolution: resolution},
		Source:     metadata.TestSource,
		Files:      files,
	}, nil); err != nil {
		return id, errors.Wrap(err, "finalize block")
	}

	if !tombstones {
		if err = os.Remove(filepath.Join(dir, id.String(), "tombstones")); err != nil {
			return id, errors.Wrap(err, "remove tombstones")
		}
	}

	return id, nil
}

var indexFilename = "index"

type indexWriterSeries struct {
	labels labels.Labels
	chunks []chunks.Meta // series file offset of chunks
}

type indexWriterSeriesSlice []*indexWriterSeries

// putOutOfOrderIndex updates the index in blockDir with an index containing an out-of-order chunk
// copied from https://github.com/prometheus/prometheus/blob/b1ed4a0a663d0c62526312311c7529471abbc565/tsdb/index/index_test.go#L346
func putOutOfOrderIndex(blockDir string, minTime int64, maxTime int64) error {

	if minTime >= maxTime || minTime+4 >= maxTime {
		return fmt.Errorf("minTime must be at least 4 less than maxTime to not create overlapping chunks")
	}

	lbls := []labels.Labels{
		[]labels.Label{
			{Name: "lbl1", Value: "1"},
		},
	}

	// Sort labels as the index writer expects series in sorted order.
	sort.Sort(labels.Slice(lbls))

	symbols := map[string]struct{}{}
	for _, lset := range lbls {
		for _, l := range lset {
			symbols[l.Name] = struct{}{}
			symbols[l.Value] = struct{}{}
		}
	}

	var input indexWriterSeriesSlice

	// Generate ChunkMetas for every label set.
	for _, lset := range lbls {
		var metas []chunks.Meta
		// only need two chunks that are out-of-order
		chk1 := chunks.Meta{
			MinTime: maxTime - 2,
			MaxTime: maxTime - 1,
			Ref:     rand.Uint64(),
			Chunk:   chunkenc.NewXORChunk(),
		}
		metas = append(metas, chk1)
		chk2 := chunks.Meta{
			MinTime: minTime + 1,
			MaxTime: minTime + 2,
			Ref:     rand.Uint64(),
			Chunk:   chunkenc.NewXORChunk(),
		}
		metas = append(metas, chk2)

		input = append(input, &indexWriterSeries{
			labels: lset,
			chunks: metas,
		})
	}

	iw, err := index.NewWriter(context.Background(), filepath.Join(blockDir, indexFilename))
	if err != nil {
		return err
	}

	syms := []string{}
	for s := range symbols {
		syms = append(syms, s)
	}
	sort.Strings(syms)
	for _, s := range syms {
		if err := iw.AddSymbol(s); err != nil {
			return err
		}
	}

	// Population procedure as done by compaction.
	var (
		postings = index.NewMemPostings()
		values   = map[string]map[string]struct{}{}
	)

	for i, s := range input {
		if err := iw.AddSeries(uint64(i), s.labels, s.chunks...); err != nil {
			return err
		}

		for _, l := range s.labels {
			valset, ok := values[l.Name]
			if !ok {
				valset = map[string]struct{}{}
				values[l.Name] = valset
			}
			valset[l.Value] = struct{}{}
		}
		postings.Add(uint64(i), s.labels)
	}

	return iw.Close()
}
