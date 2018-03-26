package bam

import (
	"fmt"
	"math"

	biogobam "github.com/biogo/hts/bam"
	"github.com/biogo/hts/bgzf"
	"github.com/biogo/hts/sam"
	"github.com/grailbio/base/file"
	"github.com/grailbio/base/vcontext"
	"github.com/grailbio/bio/biopb"
	"v.io/x/lib/vlog"
)

// UniversalRange is a range that covers all possible records.
var UniversalRange = biopb.CoordRange{
	Start: biopb.Coord{0, 0, 0},
	Limit: biopb.Coord{biopb.InfinityRefID, biopb.InfinityPos, 0},
}

// MappedRange is a range that covers all mapped records.
var MappedRange = biopb.CoordRange{
	Start: biopb.Coord{0, 0, 0},
	Limit: biopb.Coord{biopb.LimitValidRefID, biopb.InfinityPos, 0},
}

// Shard represents a genomic interval. The <StartRef,Start,StartSeq> and
// <EndRef,End,EndSeq> coordinates form a half-open, 0-based interval. The
// StartSeq, EndSeq fields are used to distinguish a list of reads at the same
// coordinate.  For a given coordinate (ref, pos), the Nth read the PAM/BAM file
// is assigned the seq value of N-1 (assuming N is 1-based). For example,
// Passing range [(startref=10,start=100,startseq=15),
// (limitref=10,limit=100,limitseq=20)] will read 16th to 20th read sequences at
// coordinate (10,100)
//
// Uses of non-zero {Start,End}Seq is supported only in PAM files. For BAM
// files, *Seq must be zero.
//
// An unmapped sequence has coordinate (nil,0,seq), and it is stored after any
// mapped sequence. Thus, a shard that contains an unmapped sequence will have
// EndRef=nil, End=1, EndSeq=0> (in theory, End can be any value > 0, but in
// practice we use End=1).
//
// Padding must be >=0. It expands the read range to [PaddedStart, PaddedEnd),
// where PaddedStart=max(0, Start-Padding) and PaddedEnd=min(EndRef.Len(),
// End+Padding)).  The regions [PaddedStart,Start) and [End,PaddedEnd) are not
// part of the shard, since the padding regions will overlap with another
// Shard's [Start, End).
//
// The Shards are ordered according to the order of the bam input file.
// ShardIdx is an index into that ordering.  The first Shard has index 0, and
// the subsequent shards increment the ShardIdx by one each.
type Shard struct {
	StartRef *sam.Reference
	EndRef   *sam.Reference
	Start    int
	End      int
	StartSeq int
	EndSeq   int

	Padding  int
	ShardIdx int
}

// UniversalShard creates a Shard that covers the entire genome, and unmapped
// reads.
func UniversalShard(header *sam.Header) Shard {
	var startRef *sam.Reference
	if len(header.Refs()) > 0 {
		startRef = header.Refs()[0]
	}
	return Shard{
		StartRef: startRef,
		EndRef:   nil,
		Start:    0,
		End:      math.MaxInt32,
	}
}

// PadStart returns max(s.Start-padding, 0).
func (s *Shard) PadStart(padding int) int {
	return max(0, s.Start-padding)
}

// PaddedStart computes the effective start of the range to read, including
// padding.
func (s *Shard) PaddedStart() int {
	return s.PadStart(s.Padding)
}

// PadEnd end returns min(s.End+padding, length of s.EndRef)
func (s *Shard) PadEnd(padding int) int {
	if s.End == 0 && s.EndSeq == 0 {
		// The shard extends to the end of the previous reference. So PadEnd can
		// stay zero.
		return 0
	}
	if s.EndRef == nil {
		// Unmapped reads are all at position 0, so limit can be any positive value.
		return min(math.MaxInt32, s.End+padding)
	}
	return min(s.EndRef.Len(), s.End+padding)
}

// PaddedEnd computes the effective limit of the range to read, including
// padding.
func (s *Shard) PaddedEnd() int {
	return s.PadEnd(s.Padding)
}

func min(x, y int) int {
	if y < x {
		return y
	}
	return x
}

func max(x, y int) int {
	if y > x {
		return y
	}
	return x
}

// ShardToCoordRange converts bam.Shard to CoordRange.
func ShardToCoordRange(shard Shard) biopb.CoordRange {
	return biopb.CoordRange{
		biopb.Coord{RefId: int32(shard.StartRef.ID()), Pos: int32(shard.Start), Seq: int32(shard.StartSeq)},
		biopb.Coord{RefId: int32(shard.EndRef.ID()), Pos: int32(shard.End), Seq: int32(shard.EndSeq)},
	}
}

// RecRangeToBAMShard converts RecRange to bam.Shard.
func CoordRangeToShard(header *sam.Header, r biopb.CoordRange, padding, shardIdx int) Shard {
	var startRef *sam.Reference
	if r.Start.RefId >= 0 {
		startRef = header.Refs()[r.Start.RefId]
	}
	var limitRef *sam.Reference
	var limitPos = int(r.Limit.Pos)
	if r.Limit.RefId >= 0 {
		if n := len(header.Refs()); int(r.Limit.RefId) < n {
			limitRef = header.Refs()[r.Limit.RefId]
			limitPos = int(r.Limit.Pos)
		} else {
			limitRef = header.Refs()[n-1]
			limitPos = limitRef.Len()
		}
	}
	return Shard{
		StartRef: startRef,
		Start:    int(r.Start.Pos),
		StartSeq: int(r.Start.Seq),
		EndRef:   limitRef,
		End:      limitPos,
		EndSeq:   int(r.Limit.Seq),
		Padding:  padding,
		ShardIdx: shardIdx,
	}
}

// CoordGenerator is a helper class for computing the Coord.Seq value from a
// sam.Record. This object must be created per pam shard. Generate() must be
// called for every record that is being read or written to the pam file in
// order.
type CoordGenerator struct {
	LastRec biopb.Coord
}

// NewCoordGenerator creates a new CoordGenerator.
func NewCoordGenerator() CoordGenerator {
	return CoordGenerator{biopb.Coord{RefId: 0, Pos: -1, Seq: 0}}
}

// Generate generates the Coord for the given (refid,pos).
//
// REQUIRES: successive calls to this function must supply a non-decreasing
// sequnece of (ref,pos) values.
func (g *CoordGenerator) Generate(refID, pos int32) biopb.Coord {
	if refID == biopb.InfinityRefID {
		// Pos for unmapped reads are meaningless.  The convention in SAM/BAM is to
		// store -1 as Pos, but we don't use negative positions elsewhere, so we
		// just use 0 as a placeholder.
		pos = 0
	}
	if refID < biopb.UnmappedRefID || pos < 0 {
		vlog.Fatalf("Illegal addr: %v %v", refID, pos)
	}
	// See if the (refid,pos) has changed. If not, increment the "Seq" part.
	a := biopb.Coord{RefId: refID, Pos: pos, Seq: 0}
	p := biopb.Coord{RefId: g.LastRec.RefId, Pos: g.LastRec.Pos, Seq: 0}
	cmp := a.Compare(p)
	if cmp < 0 {
		vlog.Fatalf("Record coordinate decreased from %+v to %v:%v", g.LastRec, refID, pos)
	}
	if cmp == 0 {
		g.LastRec.Seq++
	} else {
		g.LastRec = a
	}
	return g.LastRec
}

// GenerateFromRecord generates the Coord for the given record.
//
// REQUIRES: successive calls to this function must supply record in
// non-decreasing coordinate order.
func (g *CoordGenerator) GenerateFromRecord(rec *sam.Record) biopb.Coord {
	return g.Generate(int32(rec.Ref.ID()), int32(rec.Pos))
}

// CoordFromSAMRecord computes the biopb.Coord for the given record.  It is a
// shorthand for biopb.CoordFromCoord(rec.Ref, rec.Pos, seq).
func CoordFromSAMRecord(rec *sam.Record, seq int32) biopb.Coord {
	return NewCoord(rec.Ref, rec.Pos, seq)
}

// NewCoord generates biopb.Coord from the given parameters.
func NewCoord(ref *sam.Reference, pos int, seq int32) biopb.Coord {
	a := biopb.Coord{RefId: int32(ref.ID()), Pos: int32(pos), Seq: seq}
	if a.RefId == biopb.InfinityRefID && pos < 0 {
		// Pos for unmapped reads are meaningless.  The convention is to
		// store -1 as Pos, but we don't use negative positions
		// elsewhere, so we just use 0 as a placeholder.
		a.Pos = 0
	}
	return a
}

// NewShardChannel returns a closed channel containing the shards.
func NewShardChannel(shards []Shard) chan Shard {
	shardChan := make(chan Shard, len(shards))
	for _, shard := range shards {
		shardChan <- shard
	}
	close(shardChan)
	return shardChan
}

// GetPositionBasedShards returns a list of shards that cover the
// genome using the specified shard size and padding size.  Return a
// shard for the unmapped && mate-unmapped pairs if includeUnmapped is
// true.
//
// The Shards split the BAM data from the given provider into
// contiguous, non-overlapping genomic intervals (Shards). A SAM
// record is associated with a shard if its alignment start position
// is within the given padding distance of the shard. This means reads
// near shard boundaries may be associated with more than one shard.
func GetPositionBasedShards(header *sam.Header, shardSize int, padding int, includeUnmapped bool) ([]Shard, error) {
	var shards []Shard
	shardIdx := 0
	for _, ref := range header.Refs() {
		var start int
		for start < ref.Len() {
			end := min(start+shardSize, ref.Len())
			shards = append(shards,
				Shard{
					StartRef: ref,
					EndRef:   ref,
					Start:    start,
					End:      end,
					Padding:  padding,
					ShardIdx: shardIdx,
				})
			start += shardSize
			shardIdx++
		}
	}
	if includeUnmapped {
		shards = append(shards,
			Shard{
				StartRef: nil,
				EndRef:   nil,
				Start:    0,
				End:      math.MaxInt32,
				ShardIdx: shardIdx,
			})
	}
	ValidateShardList(shards, padding)
	return shards, nil
}

// GetByteBasedShards returns a list of shards much like
// GetPositionBasedShards, but the shards are based on a target
// bytesPerShard, and a minimum number of bases pershard (minBases).
func GetByteBasedShards(bamPath, baiPath string, bytesPerShard int64, minBases, padding int, includeUnmapped bool) ([]Shard, error) {
	type boundary struct {
		pos     int32
		filePos int64
	}
	// TODO(saito) pass the context explicitly.
	ctx := vcontext.Background()
	bamIn, err := file.Open(ctx, bamPath)
	if err != nil {
		return nil, err
	}
	defer bamIn.Close(ctx)
	bamr, err := biogobam.NewReader(bamIn.Reader(ctx), 1)
	if err != nil {
		return nil, err
	}
	header := bamr.Header()

	// Get chunks from the .bai file.
	indexIn, err := file.Open(ctx, baiPath)
	if err != nil {
		return nil, err
	}
	defer indexIn.Close(ctx)
	index, err := ReadIndex(indexIn.Reader(ctx))
	if err != nil {
		return nil, err
	}
	chunksByRef := index.AllOffsets()
	if len(chunksByRef) <= 0 {
		return nil, fmt.Errorf("%v: no chunks found in the index", baiPath)
	}

	// Compute shards
	shards := []Shard{}
	for refId := 0; refId < len(chunksByRef); refId++ {
		ref := header.Refs()[refId]
		refLen := ref.Len()
		offsets := chunksByRef[refId]

		// Pick initial shard boundaries based on bytesPerShard.
		boundaries := []boundary{}
		prevFilePos := int64(0)
		for i, offset := range offsets {
			if i == 0 || (offset.File-prevFilePos) > bytesPerShard {
				var rec biopb.Coord
				var err error
				rec, err = GetCoordAtOffset(bamr, offset)
				if err != nil {
					vlog.Fatal(err)
				}
				if int(rec.RefId) != refId {
					vlog.VI(1).Infof("No more reads in refid %d %s, filepos %d", refId, ref.Name(), offset.File)
					prevFilePos = 0
					break
				}

				if i == 0 {
					boundaries = append(boundaries, boundary{0, offset.File})
				} else {
					boundaries = append(boundaries, boundary{int32(rec.Pos), offset.File})
				}
				prevFilePos = offset.File
			}
		}

		// Some shards might be too big since the index does not cover
		// the bam file at regular intervals, so further break up
		// large shards based on genomic position.
		boundaries2 := []boundary{boundary{0, -1}}
		for i := 1; i < len(boundaries); i++ {
			if (boundaries[i].filePos - boundaries[i-1].filePos) > 2*bytesPerShard {
				genomeSubdivisions := ((boundaries[i].pos - boundaries[i-1].pos) / int32(minBases)) - 1
				bytesSubdivisions := ((boundaries[i].filePos - boundaries[i-1].filePos) / bytesPerShard) - 1
				subdivisions := int32(0)
				if int64(genomeSubdivisions) < bytesSubdivisions {
					subdivisions = int32(genomeSubdivisions)
				} else {
					subdivisions = int32(bytesSubdivisions)
				}
				if subdivisions < 0 {
					subdivisions = 0
				}

				for s := int32(1); s <= subdivisions; s++ {
					subpos := boundaries[i-1].pos + s*((boundaries[i].pos-boundaries[i-1].pos)/(subdivisions+1))
					boundaries2 = append(boundaries2, boundary{subpos, -1})
				}
			}
			boundaries2 = append(boundaries2, boundaries[i])
		}
		if boundaries2[len(boundaries2)-1].pos != int32(refLen) {
			boundaries2 = append(boundaries2, boundary{int32(refLen), -1})
		}

		// Some shards might be smaller than minBases, so break up those shards.
		boundaries3 := []boundary{boundary{0, -1}}
		for i := 1; i < len(boundaries2); i++ {
			if boundaries2[i].pos-boundaries2[i-1].pos >= int32(minBases) || i == len(boundaries2)-1 {
				boundaries3 = append(boundaries3, boundaries2[i])
			} else {
				vlog.VI(3).Infof("dropping boundary %v", boundaries2[i])
			}
		}

		// Convert boundaries to shards.
		for i := 0; i < len(boundaries3)-1; i++ {
			start := int(boundaries3[i].pos)
			end := int(boundaries3[i+1].pos)
			shards = append(shards, Shard{
				StartRef: ref,
				EndRef:   ref,
				Start:    start,
				End:      end,
				Padding:  padding,
				ShardIdx: len(shards),
			})
		}
	}
	if includeUnmapped {
		shards = append(shards, Shard{
			End:      math.MaxInt32,
			ShardIdx: len(shards),
		})
	}
	ValidateShardList(shards, padding)
	return shards, nil
}

// ValidateShardList validates that shardList has sensible values. Exposed only for testing.
func ValidateShardList(shardList []Shard, padding int) {
	var prevRef *sam.Reference
	for i, shard := range shardList {
		if shard.Start >= shard.End {
			vlog.Fatalf("Shard start must preceed end for ref %s: %d, %d", shard.StartRef.Name(), shard.Start, shard.End)
		}

		if shard.StartRef == nil {
			if i == len(shardList)-1 {
				continue
			}
			vlog.Fatalf("Only the last shard may have nil Ref, not shard %d", i)
		}

		if i == 0 || shard.StartRef != prevRef {
			prevRef = shard.StartRef
			if shard.Start != 0 {
				vlog.Fatalf("First shard of ref %s should start at 0, not %d", shard.StartRef.Name(), shard.Start)
			}
		} else {
			if shard.Start != shardList[i-1].End {
				vlog.Fatalf("Shard gap for ref %s between %d and %d", shard.StartRef.Name(), shardList[i-1].End, shard.Start)
			}
		}
		if i < len(shardList)-1 && shardList[i+1].StartRef != shard.StartRef && shard.End != shard.StartRef.Len() {
			vlog.Fatalf("Last shard of %s should end at reference end: %d, %d", shard.StartRef.Name(), shard.End, shard.StartRef.Len())
		}

		if shard.Padding < 0 {
			vlog.Fatalf("Padding must be non-negative: %d", shard.Padding)
		}
	}
}

const (
	infinityRefID = -1
)

// GetCoordAtOffset starts reading BAM from "off", and finds the first place
// where the read position increases. It returns the record
// coordinate. Coord.Seq field is always zero.
func GetCoordAtOffset(bamReader *biogobam.Reader, off bgzf.Offset) (biopb.Coord, error) {
	if off.File == 0 && off.Block == 0 {
		return biopb.Coord{RefId: 0, Pos: 0}, nil
	}
	if err := bamReader.Seek(off); err != nil {
		return biopb.Coord{}, err
	}
	rec, err := bamReader.Read()
	if err != nil {
		return biopb.Coord{}, err
	}
	c := bamReader.LastChunk()
	if c.Begin.File != off.File || c.Begin.Block != off.Block {
		err := fmt.Errorf("Corrupt BAM index %+v, bam reader offset: %+v", c, off)
		vlog.Error(err)
		return biopb.Coord{}, err
	}
	if rec.Ref.ID() > math.MaxInt32 || rec.Pos > math.MaxInt32 {
		return biopb.Coord{}, fmt.Errorf("Read coord does not fit in int32 for %v", rec)
	}
	addr := biopb.Coord{RefId: int32(rec.Ref.ID()), Pos: int32(rec.Pos)}
	if addr.RefId == infinityRefID {
		// Pos for unmapped reads are meaningless.  The convention is to
		// store -1 as Pos, but we don't use negative positions
		// elsewhere, so we just use 0 as a placeholder.
		addr.Pos = 0
	}
	return addr, nil
}
