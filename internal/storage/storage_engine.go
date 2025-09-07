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
    FormatVersion   = 2
    HeaderSize      = 32
    MetadataVersion = 1
)

type StorageEngine struct {
    filepath string
}

func NewStorageEngine(filepath string) *StorageEngine {
    return &StorageEngine{filepath: filepath}
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

    footer := se.buildFooter(timestampOffset, int64(len(timestampData)), valueOffset, int64(len(valueData)), deviceIDOffset, int64(len(deviceIDData)), metricNameOffset, int64(len(metricNameData)), len(records))
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
    binary.LittleEndian.PutUint32(header[16:20], 4)
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

func (se *StorageEngine) encodeStringColumn(strings []string) []byte {
    result := make([]byte, 0, 1024)
    countBuf := make([]byte, 4)
    binary.LittleEndian.PutUint32(countBuf, uint32(len(strings)))
    result = append(result, countBuf...)
    for _, s := range strings {
        lenBuf := make([]byte, 4)
        binary.LittleEndian.PutUint32(lenBuf, uint32(len(s)))
        result = append(result, lenBuf...)
        result = append(result, []byte(s)...)
    }
    return result
}

func (se *StorageEngine) buildFooter(timestampOffset, timestampSize, valueOffset, valueSize int64, deviceIDOffset, deviceIDSize int64, metricNameOffset, metricNameSize int64, recordCount int) []byte {
    footer := make([]byte, 0, 512)
    versionBuf := make([]byte, 4)
    binary.LittleEndian.PutUint32(versionBuf, MetadataVersion)
    footer = append(footer, versionBuf...)
    numColumnsBuf := make([]byte, 4)
    binary.LittleEndian.PutUint32(numColumnsBuf, 4)
    footer = append(footer, numColumnsBuf...)
    footer = append(footer, se.buildColumnMetadata("timestamp", 1, timestampOffset, timestampSize, recordCount)...)
    footer = append(footer, se.buildColumnMetadata("value", 1, valueOffset, valueSize, recordCount)...)
    footer = append(footer, se.buildColumnMetadata("device_id", 2, deviceIDOffset, deviceIDSize, recordCount)...)
    footer = append(footer, se.buildColumnMetadata("metric_name", 2, metricNameOffset, metricNameSize, recordCount)...)
    return footer
}

func (se *StorageEngine) buildColumnMetadata(name string, columnType uint32, offset, size int64, recordCount int) []byte {
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
