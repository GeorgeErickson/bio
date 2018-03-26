package bam_test

import (
	"testing"

	"github.com/biogo/hts/sam"
	"github.com/grailbio/bio/encoding/bam"
	"github.com/grailbio/internal/testutil"
	"github.com/stretchr/testify/assert"
)

var (
	chr8, _          = sam.NewReference("chr8", "", "", 2000000, nil, nil)
	chr9, _          = sam.NewReference("chr9", "", "", 3000000, nil, nil)
	processHeader, _ = sam.NewHeader(nil, []*sam.Reference{chr8, chr9})
	read4            = &sam.Record{
		Name:  "ABCDEFG",
		Ref:   chr8,
		Pos:   123,
		Flags: sam.Read2,
	}
	read5 = &sam.Record{
		Name:  "ABCDEFG",
		Ref:   chr8,
		Pos:   456,
		Flags: sam.Read1,
	}
	read6 = &sam.Record{
		Name:  "XYZ",
		Ref:   chr8,
		Pos:   1024,
		Flags: sam.Read1,
	}
	read7 = &sam.Record{
		Name:  "foo",
		Ref:   chr9,
		Pos:   777,
		Flags: sam.Read2,
	}
	read8 = &sam.Record{
		Name:  "foo",
		Ref:   chr9,
		Pos:   1000001,
		Flags: sam.Read1,
	}
	read9 = &sam.Record{
		Name:  "XYZ",
		Ref:   chr9,
		Pos:   2000000,
		Flags: sam.Read2,
	}
	read9Secondary = &sam.Record{
		Name:  "XYZ",
		Ref:   chr9,
		Pos:   2000002,
		Flags: sam.Read2 | sam.Secondary,
	}
	read10 = &sam.Record{
		Name:  "unmapped",
		Ref:   nil,
		Pos:   0,
		Flags: sam.Read1 | sam.Unmapped | sam.MateUnmapped,
	}
	read11 = &sam.Record{
		Name:  "unmapped",
		Ref:   nil,
		Pos:   0,
		Flags: sam.Read2 | sam.Unmapped | sam.MateUnmapped,
	}
)

func TestShard(t *testing.T) {
	ref1, err := sam.NewReference("chr1", "", "", 100, nil, nil)
	assert.NoError(t, err)
	s := bam.Shard{StartRef: ref1, Start: 20, EndRef: ref1, End: 90, Padding: 3}
	assert.Equal(t, 17, s.PaddedStart())
	assert.Equal(t, 93, s.PaddedEnd())
	assert.Equal(t, 12, s.PadStart(8))
	assert.Equal(t, 0, s.PadStart(21))
	assert.Equal(t, 100, s.PadEnd(11))

	ref2, err := sam.NewReference("chr2", "", "", 200, nil, nil)
	assert.NoError(t, err)
	s = bam.Shard{StartRef: ref1, Start: 20, EndRef: ref2, End: 0, Padding: 3}
	assert.Equal(t, 17, s.PaddedStart())
	assert.Equal(t, 0, s.PaddedEnd())

	s = bam.Shard{StartRef: ref1, Start: 20, EndRef: ref2, End: 0, Padding: 3, EndSeq: 1}
	assert.Equal(t, 17, s.PaddedStart())
	assert.Equal(t, 3, s.PaddedEnd())
}

func TestNewShardChannel(t *testing.T) {
	ref1, err := sam.NewReference("chr1", "", "", 100, nil, nil)
	assert.NoError(t, err)
	ref2, err := sam.NewReference("chr2", "", "", 101, nil, nil)
	assert.NoError(t, err)
	ref3, err := sam.NewReference("chr3", "", "", 1, nil, nil)
	assert.NoError(t, err)
	header, _ := sam.NewHeader(nil, []*sam.Reference{ref1, ref2, ref3})
	shardList, err := bam.GetPositionBasedShards(header, 50, 10, false)
	assert.NoError(t, err)
	shardChan := bam.NewShardChannel(shardList)

	shards := []bam.Shard{}
	for s := range shardChan {
		shards = append(shards, s)
	}

	assert.Equal(t, 6, len(shards))
	assert.Equal(t, shards[0], bam.Shard{ref1, ref1, 0, 50, 0, 0, 10, 0})
	assert.Equal(t, shards[1], bam.Shard{ref1, ref1, 50, 100, 0, 0, 10, 1})
	assert.Equal(t, shards[2], bam.Shard{ref2, ref2, 0, 50, 0, 0, 10, 2})
	assert.Equal(t, shards[3], bam.Shard{ref2, ref2, 50, 100, 0, 0, 10, 3})
	assert.Equal(t, shards[4], bam.Shard{ref2, ref2, 100, 101, 0, 0, 10, 4})
	assert.Equal(t, shards[5], bam.Shard{ref3, ref3, 0, 1, 0, 0, 10, 5})
}

func TestGetByteBasedShards(t *testing.T) {
	bamPath := testutil.GetFilePath("@grailgo//bio/encoding/bam/testdata/170614_WGS_LOD_Pre_Library_B3_27961B_05.merged.10000.bam")
	baiPath := testutil.GetFilePath("@grailgo//bio/encoding/bam/testdata/170614_WGS_LOD_Pre_Library_B3_27961B_05.merged.10000.bam.bai")

	shardList, err := bam.GetByteBasedShards(bamPath, baiPath, 100000, 5000, 400, true)
	assert.Nil(t, err)

	for i, shard := range shardList {
		t.Logf("shard[%d]: %v", i, shard)
	}

	bam.ValidateShardList(shardList, 400)
}
