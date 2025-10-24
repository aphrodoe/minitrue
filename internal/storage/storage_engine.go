package storage

import (
	"encoding/binary"
	"fmt"
	"os"

	"github.com/minitrue/internal/compression"
	"github.com/minitrue/internal/models"
)

const (
	MagicNumber     = 0x50415251
	FormatVersion   = 1
	HeaderSize      = 32
	MetadataVersion = 1
)

type StorageEngine struct {
	filepath string
}

func NewStorageEngine(filepath string) *StorageEngine {
	return &StorageEngine{
		filepath: filepath,
	}
}

func (se *StorageEngine) Write(records []models.Record) error {
	if len(records) == 0 {
		return fmt.Errorf("no records to write")
	}

	file, err := os.Create(se.filepath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	timestamps := make([]int64, len(records))
	values := make([]float64, len(records))
	for i, record := range records {
		timestamps[i] = record.Timestamp
		values[i] = record.Value
	}

	header := se.buildHeader(len(records))
	if _, err := file.Write(header); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	timestampOffset := int64(HeaderSize)
	compressedTimestamps := compression.CompressInt64(timestamps)
	timestampData := se.encodeCompressedColumn(compressedTimestamps)
	if _, err := file.Write(timestampData); err != nil {
		return fmt.Errorf("failed to write timestamp column: %w", err)
	}

	valueOffset := timestampOffset + int64(len(timestampData))
	compressedValues := compression.CompressFloat64(values)
	valueData := se.encodeCompressedColumn(compressedValues)
	if _, err := file.Write(valueData); err != nil {
		return fmt.Errorf("failed to write value column: %w", err)
	}

	footer := se.buildFooter(timestampOffset, int64(len(timestampData)),
		valueOffset, int64(len(valueData)), len(records))
	if _, err := file.Write(footer); err != nil {
		return fmt.Errorf("failed to write footer: %w", err)
	}

	footerSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(footerSize, uint32(len(footer)))
	if _, err := file.Write(footerSize); err != nil {
		return fmt.Errorf("failed to write footer size: %w", err)
	}

	return nil
}

func (se *StorageEngine) buildHeader(recordCount int) []byte {
	header := make([]byte, HeaderSize)
	
	binary.LittleEndian.PutUint32(header[0:4], MagicNumber)
	binary.LittleEndian.PutUint32(header[4:8], FormatVersion)
	binary.LittleEndian.PutUint64(header[8:16], uint64(recordCount))
	binary.LittleEndian.PutUint32(header[16:20], 2)
	
	copy(header[20:], []byte("TSDB"))
	
	return header
}

func (se *StorageEngine) encodeInt64Column(data []int64) []byte {
	result := make([]byte, 8+len(data)*8)
	
	binary.LittleEndian.PutUint32(result[0:4], 0)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(data)*8))
	
	for i, val := range data {
		binary.LittleEndian.PutUint64(result[8+i*8:], uint64(val))
	}
	
	return result
}

func (se *StorageEngine) encodeCompressedColumn(compressedData []byte) []byte {
	result := make([]byte, 8+len(compressedData))
	
	binary.LittleEndian.PutUint32(result[0:4], 1)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(compressedData)))
	
	copy(result[8:], compressedData)
	
	return result
}

func (se *StorageEngine) buildFooter(timestampOffset, timestampSize,
	valueOffset, valueSize int64, recordCount int) []byte {
	
	footer := make([]byte, 0, 256)
	
	versionBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(versionBuf, MetadataVersion)
	footer = append(footer, versionBuf...)
	
	numColumnsBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(numColumnsBuf, 2)
	footer = append(footer, numColumnsBuf...)
	
	timestampMeta := se.buildColumnMetadata("timestamp", 1, timestampOffset, timestampSize, recordCount)
	footer = append(footer, timestampMeta...)
	
	valueMeta := se.buildColumnMetadata("value", 1, valueOffset, valueSize, recordCount)
	footer = append(footer, valueMeta...)
	
	return footer
}

func (se *StorageEngine) buildColumnMetadata(name string, columnType uint32,
	offset, size int64, recordCount int) []byte {
	
	metadata := make([]byte, 0, 64)
	
	nameLenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(nameLenBuf, uint32(len(name)))
	metadata = append(metadata, nameLenBuf...)
	metadata = append(metadata, []byte(name)...)
	
	typeBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(typeBuf, columnType)
	metadata = append(metadata, typeBuf...)
	
	offsetBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(offsetBuf, uint64(offset))
	metadata = append(metadata, offsetBuf...)
	
	sizeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBuf, uint64(size))
	metadata = append(metadata, sizeBuf...)
	
	countBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(countBuf, uint64(recordCount))
	metadata = append(metadata, countBuf...)
	
	return metadata
}

func (se *StorageEngine) Read() ([]models.Record, error) {
	data, err := os.ReadFile(se.filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	if len(data) < HeaderSize+4 {
		return nil, fmt.Errorf("file too small")
	}

	magic := binary.LittleEndian.Uint32(data[0:4])
	if magic != MagicNumber {
		return nil, fmt.Errorf("invalid magic number")
	}

	recordCount := int(binary.LittleEndian.Uint64(data[8:16]))

	footerSizeOffset := len(data) - 4
	footerSize := binary.LittleEndian.Uint32(data[footerSizeOffset:])
	footerStart := footerSizeOffset - int(footerSize)

	footer := data[footerStart:footerSizeOffset]
	numColumns := binary.LittleEndian.Uint32(footer[4:8])
	
	if numColumns != 2 {
		return nil, fmt.Errorf("unexpected number of columns")
	}

	pos := 8
	
	timestampNameLen := binary.LittleEndian.Uint32(footer[pos : pos+4])
	pos += 4 + int(timestampNameLen)
	pos += 4
	timestampOffset := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8
	timestampSize := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8 + 8

	valueNameLen := binary.LittleEndian.Uint32(footer[pos : pos+4])
	pos += 4 + int(valueNameLen)
	pos += 4
	valueOffset := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8
	valueSize := binary.LittleEndian.Uint64(footer[pos : pos+8])

	timestampData := data[timestampOffset : timestampOffset+timestampSize]
	timestamps := se.decodeCompressedInt64Column(timestampData, recordCount)

	valueData := data[valueOffset : valueOffset+valueSize]
	values := se.decodeCompressedFloat64Column(valueData, recordCount)

	records := make([]models.Record, recordCount)
	for i := 0; i < recordCount; i++ {
		records[i] = models.Record{
			Timestamp: timestamps[i],
			Value:     values[i],
		}
	}

	return records, nil
}

func (se *StorageEngine) decodeInt64Column(data []byte, count int) []int64 {
	result := make([]int64, count)
	for i := 0; i < count; i++ {
		result[i] = int64(binary.LittleEndian.Uint64(data[8+i*8:]))
	}
	return result
}

func (se *StorageEngine) decodeCompressedInt64Column(data []byte, count int) []int64 {
	compressedData := data[8:]
	return compression.DecompressInt64(compressedData, count)
}

func (se *StorageEngine) decodeCompressedFloat64Column(data []byte, count int) []float64 {
	compressedData := data[8:]
	return compression.DecompressFloat64(compressedData, count)
}

