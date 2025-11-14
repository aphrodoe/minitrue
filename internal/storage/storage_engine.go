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
	FormatVersion   = 2 // Version 2 includes device_id and metric_name
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
	deviceIDs := make([]string, len(records))
	metricNames := make([]string, len(records))
	for i, record := range records {
		timestamps[i] = record.Timestamp
		values[i] = record.Value
		deviceIDs[i] = record.DeviceID
		metricNames[i] = record.MetricName
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

	deviceIDOffset := valueOffset + int64(len(valueData))
	deviceIDData := se.encodeStringColumn(deviceIDs)
	if _, err := file.Write(deviceIDData); err != nil {
		return fmt.Errorf("failed to write device_id column: %w", err)
	}

	metricNameOffset := deviceIDOffset + int64(len(deviceIDData))
	metricNameData := se.encodeStringColumn(metricNames)
	if _, err := file.Write(metricNameData); err != nil {
		return fmt.Errorf("failed to write metric_name column: %w", err)
	}

	footer := se.buildFooter(timestampOffset, int64(len(timestampData)),
		valueOffset, int64(len(valueData)),
		deviceIDOffset, int64(len(deviceIDData)),
		metricNameOffset, int64(len(metricNameData)),
		len(records))
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
	binary.LittleEndian.PutUint32(header[16:20], 4) // 4 columns: timestamp, value, device_id, metric_name
	copy(header[20:], []byte("TSDB"))
	return header
}

func (se *StorageEngine) encodeCompressedColumn(compressedData []byte) []byte {
	result := make([]byte, 8+len(compressedData))
	
	binary.LittleEndian.PutUint32(result[0:4], 1)
	binary.LittleEndian.PutUint32(result[4:8], uint32(len(compressedData)))
	
	copy(result[8:], compressedData)
	
	return result
}

// encodeStringColumn encodes a slice of strings as length-prefixed strings
func (se *StorageEngine) encodeStringColumn(strings []string) []byte {
	result := make([]byte, 0, 1024)
	
	// Write number of strings
	countBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(countBuf, uint32(len(strings)))
	result = append(result, countBuf...)
	
	// Write each string as length-prefixed
	for _, s := range strings {
		lenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBuf, uint32(len(s)))
		result = append(result, lenBuf...)
		result = append(result, []byte(s)...)
	}
	
	return result
}

// decodeStringColumn decodes a slice of strings from length-prefixed format
func (se *StorageEngine) decodeStringColumn(data []byte, count int) ([]string, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("insufficient data for string column")
	}
	
	pos := 0
	stringCount := int(binary.LittleEndian.Uint32(data[pos:pos+4]))
	pos += 4
	
	if stringCount != count {
		return nil, fmt.Errorf("string count mismatch: expected %d, got %d", count, stringCount)
	}
	
	strings := make([]string, 0, count)
	for i := 0; i < count; i++ {
		if pos+4 > len(data) {
			return nil, fmt.Errorf("insufficient data for string length at index %d", i)
		}
		
		strLen := int(binary.LittleEndian.Uint32(data[pos:pos+4]))
		pos += 4
		
		if pos+strLen > len(data) {
			return nil, fmt.Errorf("insufficient data for string at index %d", i)
		}
		
		strings = append(strings, string(data[pos:pos+strLen]))
		pos += strLen
	}
	
	return strings, nil
}

func (se *StorageEngine) buildFooter(timestampOffset, timestampSize,
	valueOffset, valueSize int64,
	deviceIDOffset, deviceIDSize int64,
	metricNameOffset, metricNameSize int64,
	recordCount int) []byte {
	footer := make([]byte, 0, 512)
	
	versionBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(versionBuf, MetadataVersion)
	footer = append(footer, versionBuf...)
	
	numColumnsBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(numColumnsBuf, 4) // 4 columns
	footer = append(footer, numColumnsBuf...)
	
	timestampMeta := se.buildColumnMetadata("timestamp", 1, timestampOffset, timestampSize, recordCount)
	footer = append(footer, timestampMeta...)
	
	valueMeta := se.buildColumnMetadata("value", 1, valueOffset, valueSize, recordCount)
	footer = append(footer, valueMeta...)
	
	deviceIDMeta := se.buildColumnMetadata("device_id", 2, deviceIDOffset, deviceIDSize, recordCount)
	footer = append(footer, deviceIDMeta...)
	
	metricNameMeta := se.buildColumnMetadata("metric_name", 2, metricNameOffset, metricNameSize, recordCount)
	footer = append(footer, metricNameMeta...)
	
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

	formatVersion := binary.LittleEndian.Uint32(data[4:8])
	recordCount := int(binary.LittleEndian.Uint64(data[8:16]))

	footerSizeOffset := len(data) - 4
	footerSize := binary.LittleEndian.Uint32(data[footerSizeOffset:])
	footerStart := footerSizeOffset - int(footerSize)

	footer := data[footerStart:footerSizeOffset]
	numColumns := binary.LittleEndian.Uint32(footer[4:8])
	
	// Support both version 1 (2 columns) and version 2 (4 columns)
	if numColumns != 2 && numColumns != 4 {
		return nil, fmt.Errorf("unexpected number of columns: %d", numColumns)
	}

	pos := 8
	
	// Read timestamp column
	timestampNameLen := binary.LittleEndian.Uint32(footer[pos : pos+4])
	pos += 4 + int(timestampNameLen)
	pos += 4
	timestampOffset := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8
	timestampSize := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8 + 8

	// Read value column
	valueNameLen := binary.LittleEndian.Uint32(footer[pos : pos+4])
	pos += 4 + int(valueNameLen)
	pos += 4
	valueOffset := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8
	valueSize := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8 + 8

	timestampData := data[timestampOffset : timestampOffset+timestampSize]
	timestamps := se.decodeCompressedInt64Column(timestampData, recordCount)

	valueData := data[valueOffset : valueOffset+valueSize]
	values := se.decodeCompressedFloat64Column(valueData, recordCount)

	records := make([]models.Record, recordCount)
	
	// Handle version 1 files (no device_id/metric_name)
	if formatVersion == 1 || numColumns == 2 {
		for i := 0; i < recordCount; i++ {
			records[i] = models.Record{
				Timestamp:  timestamps[i],
				Value:      values[i],
				DeviceID:   "", // Empty for version 1 files
				MetricName: "", // Empty for version 1 files
			}
		}
		return records, nil
	}
	
	// Handle version 2 files (with device_id/metric_name)
	deviceIDNameLen := binary.LittleEndian.Uint32(footer[pos : pos+4])
	pos += 4 + int(deviceIDNameLen)
	pos += 4
	deviceIDOffset := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8
	deviceIDSize := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8 + 8

	metricNameNameLen := binary.LittleEndian.Uint32(footer[pos : pos+4])
	pos += 4 + int(metricNameNameLen)
	pos += 4
	metricNameOffset := binary.LittleEndian.Uint64(footer[pos : pos+8])
	pos += 8
	metricNameSize := binary.LittleEndian.Uint64(footer[pos : pos+8])

	deviceIDData := data[deviceIDOffset : deviceIDOffset+deviceIDSize]
	deviceIDs, err := se.decodeStringColumn(deviceIDData, recordCount)
	if err != nil {
		return nil, fmt.Errorf("failed to decode device_id column: %w", err)
	}

	metricNameData := data[metricNameOffset : metricNameOffset+metricNameSize]
	metricNames, err := se.decodeStringColumn(metricNameData, recordCount)
	if err != nil {
		return nil, fmt.Errorf("failed to decode metric_name column: %w", err)
	}

	for i := 0; i < recordCount; i++ {
		records[i] = models.Record{
			Timestamp:  timestamps[i],
			Value:      values[i],
			DeviceID:   deviceIDs[i],
			MetricName: metricNames[i],
		}
	}

	return records, nil
}

func (se *StorageEngine) decodeCompressedInt64Column(data []byte, count int) []int64 {
	compressedData := data[8:]
	return compression.DecompressInt64(compressedData, count)
}

func (se *StorageEngine) decodeCompressedFloat64Column(data []byte, count int) []float64 {
	compressedData := data[8:]
	return compression.DecompressFloat64(compressedData, count)
}

