// Copyright 2018 GRAIL, Inc. All rights reserved.
// Use of this source code is governed by the Apache 2.0
// license that can be found in the LICENSE file.

package pam

import (
	"testing"

	"github.com/grailbio/bio/biopb"
	gbam "github.com/grailbio/bio/encoding/bam"
	"github.com/stretchr/testify/assert"
)

func TestFieldType(t *testing.T) {
	assert.Equal(t, gbam.FieldAux.String(), "aux")
	f, err := gbam.ParseFieldType("aux")
	assert.NoError(t, err)
	assert.Equal(t, f, gbam.FieldAux)
	f, err = gbam.ParseFieldType("coord")
	assert.NoError(t, err)
	assert.Equal(t, f, gbam.FieldCoord)
}

func TestRecAddr(t *testing.T) {
	tests := []struct {
		r0, r1 biopb.Coord
		lt     bool
		ge     bool
	}{
		{biopb.Coord{0, 1, 0}, biopb.Coord{0, 1, 0}, false, true}, // equal
		{biopb.Coord{0, 1, 0}, biopb.Coord{0, 2, 0}, true, false},
		{biopb.Coord{0, 2, 0}, biopb.Coord{0, 1, 0}, false, true},
		{biopb.Coord{0, 2, 0}, biopb.Coord{biopb.InfinityRefID, 2, 0}, true, false},
		{biopb.Coord{0, 2, 0}, biopb.Coord{1, 2, 0}, true, false},
		// {biopb.Coord{0, 709305, 0}, biopb.Coord{0, 44478570, 0}, true, false},
	}
	for _, test := range tests {
		assert.Equal(t, test.r0.LT(test.r1), test.lt, "LT: %+v", test)
		assert.Equal(t, test.r0.GE(test.r1), test.ge, "GE: %+v", test)
	}
}

func TestRecRangeIntersects(t *testing.T) {
	tests := []struct {
		startRefid0, startPos0, limitRefid0, limitPos0,
		startRefid1, startPos1, limitRefid1, limitPos1 int32
		intersect bool
	}{
		{3, 2, 10, 5, 10, 4, 11, 0, true},
		{3, 2, 10, 5, 10, 5, 11, 0, false},
		{3, 2, 10, 5, 0, 0, 3, 2, false},
		{3, 2, 10, 5, 0, 0, 3, 3, true},
	}
	for _, test := range tests {
		r0 := biopb.CoordRange{biopb.Coord{test.startRefid0, test.startPos0, 0},
			biopb.Coord{test.limitRefid0, test.limitPos0, 0}}
		r1 := biopb.CoordRange{biopb.Coord{test.startRefid1, test.startPos1, 0},
			biopb.Coord{test.limitRefid1, test.limitPos1, 0}}
		assert.Equal(t, r0.Intersects(r1), test.intersect, test)
	}
}

func TestRecRangeContains(t *testing.T) {
	tests := []struct {
		a        biopb.Coord
		contains bool
	}{
		{biopb.Coord{10, 19, 0}, false},
		{biopb.Coord{10, 20, 0}, true},
		{biopb.Coord{12, 0, 0}, true},
		{biopb.Coord{15, 4, 0}, true},
		{biopb.Coord{15, 5, 0}, false},
	}
	r := biopb.CoordRange{biopb.Coord{10, 20, 0}, biopb.Coord{15, 5, 0}}
	for _, test := range tests {
		assert.Equal(t, r.Contains(test.a), test.contains, test)
	}
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		path                         string
		expectedType                 FileType
		expectedDir                  string
		expectedStart, expectedLimit biopb.Coord
		expectedField                gbam.FieldType
		expectError                  bool
	}{
		{"foo/0:0,-:0.index", FileTypeShardIndex, "foo", biopb.Coord{0, 0, 0}, biopb.Coord{-1, 0, 0}, gbam.FieldInvalid, false},
		{"foo/0:0:10,-:0:11.index", FileTypeShardIndex, "foo", biopb.Coord{0, 0, 10}, biopb.Coord{-1, 0, 11}, gbam.FieldInvalid, false},
		{"foo/-:123,-:234.index", FileTypeShardIndex, "foo", biopb.Coord{-1, 123, 0}, biopb.Coord{-1, 234, 0}, gbam.FieldInvalid, false},
		{"foo/3:123,4:234.index", FileTypeShardIndex, "foo", biopb.Coord{3, 123, 0}, biopb.Coord{4, 234, 0}, gbam.FieldInvalid, false},
		{"foo/3:123,4:234.aux", FileTypeFieldData, "foo", biopb.Coord{3, 123, 0}, biopb.Coord{4, 234, 0}, gbam.FieldAux, false},
		{"foo", FileTypeUnknown, "", biopb.Coord{0, 0, 0}, biopb.Coord{0, 0, 0}, gbam.FieldInvalid, true},
		{"s3://foo.bar/0:0,-:0.index", FileTypeShardIndex, "s3://foo.bar", biopb.Coord{0, 0, 0}, biopb.Coord{-1, 0, 0}, gbam.FieldInvalid, false},
		{"/foo.bar/0:0,-:0.index", FileTypeShardIndex, "/foo.bar", biopb.Coord{0, 0, 0}, biopb.Coord{-1, 0, 0}, gbam.FieldInvalid, false},
	}
	for _, test := range tests {
		fi, err := ParsePath(test.path)
		if test.expectError {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, fi.Type, test.expectedType, "Test", test)
		if fi.Type != FileTypeUnknown {
			assert.Equal(t, fi.Dir, test.expectedDir, "Test", test)
		}
		assert.Equal(t, fi.Range.Start, test.expectedStart, "Test", test)
		assert.Equal(t, fi.Range.Limit, test.expectedLimit, "Test", test)
		if test.expectedField != gbam.FieldInvalid {
			assert.Equal(t, fi.Field, test.expectedField, "Test", test)
		}
	}
}
