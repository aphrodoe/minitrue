package storage

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
)

// GetSegmentDigest reads the record count and checksum from a segment file.
func GetSegmentDigest(filepath string) (uint32, int, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) < HeaderSize+4 {
		return 0, 0, fmt.Errorf("file too small")
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != MagicNumber {
		return 0, 0, fmt.Errorf("invalid magic number")
	}

	formatVersion := binary.LittleEndian.Uint32(data[4:8])
	recordCount := int(binary.LittleEndian.Uint64(data[8:16]))

	footerSizeOffset := len(data) - 4
	footerSize := binary.LittleEndian.Uint32(data[footerSizeOffset:])
	footerStart := footerSizeOffset - int(footerSize)
	if footerSize == 0 || footerStart < HeaderSize || footerStart > footerSizeOffset {
		return 0, 0, fmt.Errorf("%w: invalid footer size", ErrCorruptSegment)
	}

	footer := data[footerStart:footerSizeOffset]
	var checksum uint32
	if formatVersion >= 3 {
		if len(footer) < 12 {
			return 0, 0, fmt.Errorf("%w: footer too small for checksum", ErrCorruptSegment)
		}
		checksum = binary.LittleEndian.Uint32(footer[len(footer)-4:])
	} else {
		checksum = crc32.ChecksumIEEE(data[HeaderSize:footerStart])
	}

	return checksum, recordCount, nil
}
