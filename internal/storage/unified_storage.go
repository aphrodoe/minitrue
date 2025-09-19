package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/minitrue/internal/models"
)

type Storage interface {
	PersistPrimary(p interface{}) error
	PersistReplica(p interface{}) error
	Query(deviceID, metric string, start, end int64) ([]float64, error)
	QueryAggregated(deviceID, metric string, start, end int64) (QueryStats, error)
	Delete(deviceID, metric string) error
	Reload() error
	GetSeriesDigest(deviceID, metric string) (uint32, int, error)
	GetSeriesRecords(deviceID, metric string) ([]models.Record, error)
	GetOwnedSeriesKeys() []string
}

type QueryStats struct {
	Sum   float64
	Count int
	Min   float64
	Max   float64
}

type sample struct {
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
	Role      string  `json:"role"`
}

type Chunk struct {
	StartTime int64
	EndTime   int64
	Sum       float64
	Min       float64
	Max       float64
	Count     int
	Samples   []sample
}

func (c *Chunk) Insert(newSample sample) {
	if c.Count == 0 {
		c.StartTime = newSample.Timestamp
		c.EndTime = newSample.Timestamp
		c.Min = newSample.Value
		c.Max = newSample.Value
		c.Sum = newSample.Value
		c.Count = 1
		c.Samples = append(c.Samples, newSample)
		return
	}

	insertPos := sort.Search(len(c.Samples), func(i int) bool {
		return c.Samples[i].Timestamp >= newSample.Timestamp
	})

	if insertPos < len(c.Samples) && c.Samples[insertPos].Timestamp == newSample.Timestamp {
		return // Idempotent: ignore duplicate timestamps
	}

	if insertPos == len(c.Samples) {
		c.Samples = append(c.Samples, newSample)
	} else {
		c.Samples = append(c.Samples, sample{})
		copy(c.Samples[insertPos+1:], c.Samples[insertPos:])
		c.Samples[insertPos] = newSample
	}

	if newSample.Timestamp < c.StartTime {
		c.StartTime = newSample.Timestamp
	}
	if newSample.Timestamp > c.EndTime {
		c.EndTime = newSample.Timestamp
	}
	if newSample.Value < c.Min {
		c.Min = newSample.Value
	}
	if newSample.Value > c.Max {
		c.Max = newSample.Value
	}
	c.Sum += newSample.Value
	c.Count++
}

type Series struct {
	Chunks []*Chunk
}

func (s *Series) Insert(newSample sample) {
	if len(s.Chunks) == 0 {
		c := &Chunk{}
		c.Insert(newSample)
		s.Chunks = append(s.Chunks, c)
		return
	}

	lastChunk := s.Chunks[len(s.Chunks)-1]

	// Handle massively delayed out-of-order writes
	if newSample.Timestamp < lastChunk.StartTime {
		idx := sort.Search(len(s.Chunks), func(i int) bool {
			return s.Chunks[i].EndTime >= newSample.Timestamp
		})
		if idx < len(s.Chunks) {
			s.Chunks[idx].Insert(newSample)
			return
		}
	}

	// 1000 points per chunk bounds the Edge Binary Search to <1000 items
	if lastChunk.Count >= 1000 {
		c := &Chunk{}
		c.Insert(newSample)
		s.Chunks = append(s.Chunks, c)
	} else {
		lastChunk.Insert(newSample)
	}
}

type UnifiedStorage struct {
	mu                         sync.RWMutex
	data                       map[string]*Series
	file                       *os.File
	engine                     *StorageEngine
	filepath                   string
	segmentDir                 string
	batchSize                  int
	batch                      []models.Record
	nodeID                     string
	lastFlush                  time.Time
	nextSegmentSeq             map[string]int
	compactionInterval         time.Duration
	compactionSegmentThreshold int
}

func NewUnifiedStorage(filepath string) *UnifiedStorage {
	nodeID := "unknown"
	base := filepath
	// Handle both forward and backward path separators
	if lastSlash := len(filepath) - 1; lastSlash >= 0 {
		for i := lastSlash; i >= 0; i-- {
			if filepath[i] == '/' || filepath[i] == '\\' {
				base = filepath[i+1:]
				break
			}
		}
	}
	if dotIdx := 0; dotIdx < len(base) {
		for i := 0; i < len(base); i++ {
			if base[i] == '.' {
				nodeID = base[:i]
				break
			}
		}
		if nodeID == "unknown" {
			nodeID = base
		}
	}

	storage := &UnifiedStorage{
		data:                       make(map[string]*Series),
		file:                       nil,
		engine:                     NewStorageEngine(filepath),
		filepath:                   filepath,
		segmentDir:                 defaultSegmentDir(filepath),
		batchSize:                  10,
		batch:                      make([]models.Record, 0, 10),
		nodeID:                     nodeID,
		lastFlush:                  time.Now(),
		nextSegmentSeq:             make(map[string]int),
		compactionInterval:         2 * time.Minute,
		compactionSegmentThreshold: 10,
	}

	if err := os.MkdirAll(storage.segmentDir, 0755); err != nil {
		log.Printf("[Storage-%s] Warning: Failed to create segment directory %s: %v", nodeID, storage.segmentDir, err)
	}

	if err := storage.Reload(); err != nil {
		log.Printf("[Storage-%s] Warning: Failed to reload data from disk: %v", nodeID, err)
	}

	go storage.periodicFlush()
	go storage.periodicCompaction()

	return storage
}

func defaultSegmentDir(storagePath string) string {
	ext := filepath.Ext(storagePath)
	if ext == "" {
		return storagePath + "_segments"
	}
	return strings.TrimSuffix(storagePath, ext) + "_segments"
}

func seriesSegmentID(seriesKey string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(seriesKey))
}

func seriesKeyFromSegmentID(seriesID string) (string, bool) {
	decoded, err := base64.RawURLEncoding.DecodeString(seriesID)
	if err != nil {
		return "", false
	}
	return string(decoded), true
}

func (m *UnifiedStorage) segmentPath(seriesKey string, sequence int) string {
	filename := fmt.Sprintf("%s-%020d.parq", seriesSegmentID(seriesKey), sequence)
	return filepath.Join(m.segmentDir, filename)
}

func parseSegmentFilename(path string) (string, int, bool) {
	base := filepath.Base(path)
	if filepath.Ext(base) != ".parq" {
		return "", 0, false
	}
	name := strings.TrimSuffix(base, ".parq")
	sep := strings.LastIndex(name, "-")
	if sep < 0 {
		return "", 0, false
	}
	seriesID := name[:sep]
	sequence, err := strconv.Atoi(name[sep+1:])
	if err != nil {
		return "", 0, false
	}
	return seriesID, sequence, true
}

func (m *UnifiedStorage) segmentFiles() ([]string, error) {
	files, err := filepath.Glob(filepath.Join(m.segmentDir, "*.parq"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func (m *UnifiedStorage) segmentFilesForSeries(seriesKey string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(m.segmentDir, seriesSegmentID(seriesKey)+"-*.parq"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func (m *UnifiedStorage) compactSegments() error {
	threshold := m.compactionSegmentThreshold
	if threshold <= 0 {
		return nil
	}

	files, err := m.segmentFiles()
	if err != nil {
		return err
	}

	filesBySeriesID := make(map[string][]string)
	for _, file := range files {
		seriesID, _, ok := parseSegmentFilename(file)
		if !ok {
			continue
		}
		filesBySeriesID[seriesID] = append(filesBySeriesID[seriesID], file)
	}

	for seriesID, seriesFiles := range filesBySeriesID {
		if len(seriesFiles) <= threshold {
			continue
		}
		if err := m.compactSeriesSegments(seriesID, seriesFiles); err != nil {
			return err
		}
	}

	return nil
}

func (m *UnifiedStorage) compactSeriesSegments(seriesID string, seriesFiles []string) error {
	seriesKey, ok := seriesKeyFromSegmentID(seriesID)
	if !ok {
		return fmt.Errorf("invalid segment series id %q", seriesID)
	}

	sort.Strings(seriesFiles)
	records := make([]models.Record, 0)
	maxSequence := 0
	for _, segmentFile := range seriesFiles {
		_, sequence, ok := parseSegmentFilename(segmentFile)
		if ok && sequence > maxSequence {
			maxSequence = sequence
		}
		segmentRecords, err := NewStorageEngine(segmentFile).Read()
		if err != nil {
			return fmt.Errorf("failed to read segment %s during compaction: %w", segmentFile, err)
		}
		records = append(records, segmentRecords...)
	}
	if len(records) == 0 {
		return nil
	}

	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Timestamp < records[j].Timestamp
	})

	m.mu.Lock()
	sequence := m.nextSegmentSeq[seriesKey]
	if sequence <= maxSequence {
		sequence = maxSequence + 1
	}
	m.nextSegmentSeq[seriesKey] = sequence + 1
	m.mu.Unlock()

	finalPath := m.segmentPath(seriesKey, sequence)
	tempPath := finalPath + ".tmp"
	if err := NewStorageEngine(tempPath).Write(records); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to write compacted segment %s: %w", tempPath, err)
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to install compacted segment %s: %w", finalPath, err)
	}

	for _, segmentFile := range seriesFiles {
		if err := os.Remove(segmentFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove compacted source segment %s: %w", segmentFile, err)
		}
	}

	log.Printf("[Storage-%s] Compacted %d segments for %s into %s", m.nodeID, len(seriesFiles), seriesKey, finalPath)
	return nil
}

func (m *UnifiedStorage) periodicFlush() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.Lock()
		if len(m.batch) > 0 {
			log.Printf("[Storage-%s] Periodic flush: %d records", m.nodeID, len(m.batch))
			m.flushBatchUnlocked()
		}
		m.mu.Unlock()
	}
}

func (m *UnifiedStorage) periodicCompaction() {
	if m.compactionInterval <= 0 {
		return
	}

	ticker := time.NewTicker(m.compactionInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := m.compactSegments(); err != nil {
			log.Printf("[Storage-%s] Compaction error: %v", m.nodeID, err)
		}
	}
}

func (m *UnifiedStorage) PersistPrimary(p interface{}) error {
	return m.persist(p, "primary")
}

func (m *UnifiedStorage) PersistReplica(p interface{}) error {
	return m.persist(p, "replica")
}

func (m *UnifiedStorage) persist(p interface{}, role string) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}

	var mp map[string]interface{}
	if err := json.Unmarshal(b, &mp); err != nil {
		return err
	}
	device, _ := mp["device_id"].(string)
	metric, _ := mp["metric_name"].(string)

	ts := int64(0)
	if v, ok := mp["timestamp"].(float64); ok {
		ts = int64(v)
	} else if v, ok := mp["timestamp"].(int64); ok {
		ts = v
	} else {
		ts = time.Now().Unix()
	}

	val := 0.0
	if v, ok := mp["value"].(float64); ok {
		val = v
	}

	key := device + "|" + metric

	m.mu.Lock()
	newSample := sample{Timestamp: ts, Value: val, Role: role}
	series, ok := m.data[key]
	if !ok {
		series = &Series{}
		m.data[key] = series
	}
	series.Insert(newSample)

	if role == "primary" {
		m.batch = append(m.batch, models.Record{
			Timestamp:  ts,
			Value:      val,
			DeviceID:   device,
			MetricName: metric,
		})
		if len(m.batch) >= m.batchSize {
			log.Printf("[Storage-%s] Batch full: flushing %d records", m.nodeID, len(m.batch))
			m.flushBatchUnlocked()
		}
	}
	m.mu.Unlock()
	return nil
}

func (m *UnifiedStorage) flushBatchUnlocked() {
	if len(m.batch) == 0 {
		return
	}
	type segmentWrite struct {
		seriesKey string
		sequence  int
		records   []models.Record
	}

	writesBySeries := make(map[string]*segmentWrite)
	for _, record := range m.batch {
		seriesKey := record.DeviceID + "|" + record.MetricName
		write, ok := writesBySeries[seriesKey]
		if !ok {
			sequence := m.nextSegmentSeq[seriesKey]
			if sequence == 0 {
				sequence = 1
			}
			m.nextSegmentSeq[seriesKey] = sequence + 1
			write = &segmentWrite{seriesKey: seriesKey, sequence: sequence}
			writesBySeries[seriesKey] = write
		}
		write.records = append(write.records, record)
	}

	writes := make([]segmentWrite, 0, len(writesBySeries))
	for _, write := range writesBySeries {
		writes = append(writes, *write)
	}
	sort.Slice(writes, func(i, j int) bool {
		return writes[i].seriesKey < writes[j].seriesKey
	})

	m.batch = m.batch[:0]
	m.lastFlush = time.Now()

	m.mu.Unlock()

	if err := os.MkdirAll(m.segmentDir, 0755); err != nil {
		log.Printf("[Storage-%s] ERROR creating segment directory %s: %v", m.nodeID, m.segmentDir, err)
		m.mu.Lock()
		return
	}

	for _, write := range writes {
		sort.Slice(write.records, func(i, j int) bool {
			return write.records[i].Timestamp < write.records[j].Timestamp
		})

		segmentPath := m.segmentPath(write.seriesKey, write.sequence)
		if err := NewStorageEngine(segmentPath).Write(write.records); err != nil {
			log.Printf("[Storage-%s] ERROR writing segment %s: %v", m.nodeID, segmentPath, err)
		} else {
			log.Printf("[Storage-%s] Wrote immutable segment %s with %d records", m.nodeID, segmentPath, len(write.records))
		}
	}

	m.mu.Lock()
}

func binarySearchStart(arr []sample, start int64) int {
	return sort.Search(len(arr), func(i int) bool {
		return arr[i].Timestamp >= start
	})
}

func binarySearchEnd(arr []sample, end int64) int {
	idx := sort.Search(len(arr), func(i int) bool {
		return arr[i].Timestamp > end
	})
	if idx > 0 {
		return idx - 1
	}
	return -1
}

func (m *UnifiedStorage) Query(deviceID, metric string, start, end int64) ([]float64, error) {
	key := deviceID + "|" + metric
	m.mu.RLock()
	series, ok := m.data[key]
	m.mu.RUnlock()

	if !ok || len(series.Chunks) == 0 {
		log.Printf("[Storage-%s] Query for %s returned 0 points", m.nodeID, key)
		return []float64{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if start == 0 {
		start = 0
	}
	if end == 0 {
		end = math.MaxInt64
	}

	var res []float64

	for _, chunk := range series.Chunks {
		if chunk.EndTime < start || chunk.StartTime > end {
			continue
		}

		startIdx := binarySearchStart(chunk.Samples, start)
		if startIdx >= len(chunk.Samples) {
			continue
		}

		endIdx := binarySearchEnd(chunk.Samples, end)
		if endIdx < startIdx {
			continue
		}

		for i := startIdx; i <= endIdx; i++ {
			res = append(res, chunk.Samples[i].Value)
		}
	}

	return res, nil
}

func (m *UnifiedStorage) QueryAggregated(deviceID, metric string, start, end int64) (QueryStats, error) {
	key := deviceID + "|" + metric
	m.mu.RLock()
	series, ok := m.data[key]
	m.mu.RUnlock()

	if !ok || len(series.Chunks) == 0 {
		return QueryStats{}, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if start == 0 {
		start = 0
	}
	if end == 0 {
		end = math.MaxInt64
	}

	var totalSum float64
	var totalCount int
	var min, max float64
	firstPoint := true

	updateStats := func(val float64) {
		totalSum += val
		totalCount++
		if firstPoint {
			min = val
			max = val
			firstPoint = false
		} else {
			if val < min {
				min = val
			}
			if val > max {
				max = val
			}
		}
	}

	for _, chunk := range series.Chunks {
		if chunk.EndTime < start || chunk.StartTime > end {
			continue
		}

		// Case A: Chunk falls ENTIRELY within the requested time range.
		// O(1) Pre-Aggregated operation! We never touch the raw data arrays.
		if chunk.StartTime >= start && chunk.EndTime <= end {
			totalSum += chunk.Sum
			totalCount += chunk.Count
			if firstPoint {
				min = chunk.Min
				max = chunk.Max
				firstPoint = false
			} else {
				if chunk.Min < min {
					min = chunk.Min
				}
				if chunk.Max > max {
					max = chunk.Max
				}
			}
			continue
		}

		// Case B: Chunk lies on the edge of the requested time range.
		// O(log N) + O(K) fallback where we aggregate just this boundary chunk.
		startIdx := binarySearchStart(chunk.Samples, start)
		if startIdx >= len(chunk.Samples) {
			continue
		}

		endIdx := binarySearchEnd(chunk.Samples, end)
		if endIdx < startIdx {
			continue
		}

		for i := startIdx; i <= endIdx; i++ {
			updateStats(chunk.Samples[i].Value)
		}
	}

	return QueryStats{Sum: totalSum, Count: totalCount, Min: min, Max: max}, nil
}

func (m *UnifiedStorage) Delete(deviceID, metric string) error {
	key := deviceID + "|" + metric

	m.mu.Lock()
	delete(m.data, key)
	delete(m.nextSegmentSeq, key)

	if len(m.batch) > 0 {
		originalBatchSize := len(m.batch)
		filteredBatch := make([]models.Record, 0, len(m.batch))
		for _, record := range m.batch {
			if record.DeviceID != deviceID || record.MetricName != metric {
				filteredBatch = append(filteredBatch, record)
			}
		}
		m.batch = filteredBatch
		removedCount := originalBatchSize - len(filteredBatch)
		if removedCount > 0 {
			log.Printf("[Storage-%s] Filtered batch: removed %d records for deleted device/metric", m.nodeID, removedCount)
		}
	}
	m.mu.Unlock()

	segmentFiles, err := m.segmentFilesForSeries(key)
	if err != nil {
		return fmt.Errorf("failed to list segment files for deletion: %w", err)
	}
	for _, segmentFile := range segmentFiles {
		if err := os.Remove(segmentFile); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove segment %s: %w", segmentFile, err)
		}
	}
	if len(segmentFiles) > 0 {
		log.Printf("[Storage-%s] Removed %d segment files for device=%s metric=%s", m.nodeID, len(segmentFiles), deviceID, metric)
	}

	existing, err := m.engine.Read()
	if err != nil {
		log.Printf("[Storage-%s] Could not read disk file for deletion: %v", m.nodeID, err)
	} else {
		filteredRecords := make([]models.Record, 0, len(existing))
		for _, record := range existing {
			if record.DeviceID == "" && record.MetricName == "" {
				filteredRecords = append(filteredRecords, record)
			} else if record.DeviceID != deviceID || record.MetricName != metric {
				filteredRecords = append(filteredRecords, record)
			}
		}

		if len(filteredRecords) == 0 {
			if err := os.Remove(m.filepath); err != nil && !os.IsNotExist(err) {
				log.Printf("[Storage-%s] Error removing empty file: %v", m.nodeID, err)
			} else {
				log.Printf("[Storage-%s] Removed empty disk file after deletion", m.nodeID)
			}
		} else {
			if err := m.engine.Write(filteredRecords); err != nil {
				log.Printf("[Storage-%s] Error writing filtered records to disk: %v", m.nodeID, err)
			} else {
				log.Printf("[Storage-%s] Wrote %d filtered records to disk", m.nodeID, len(filteredRecords))
			}
		}
	}

	log.Printf("[Storage-%s] Deleted all data for device=%s metric=%s (memory and disk)", m.nodeID, deviceID, metric)

	return nil
}

func (m *UnifiedStorage) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data = make(map[string]*Series)
	m.nextSegmentSeq = make(map[string]int)

	records := make([]models.Record, 0)
	legacyRecords, err := m.engine.Read()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log.Printf("[Storage-%s] No legacy data file, checking segments", m.nodeID)
		} else {
			var pathErr *os.PathError
			if errors.As(err, &pathErr) && errors.Is(pathErr.Err, os.ErrNotExist) {
				log.Printf("[Storage-%s] No legacy data file, checking segments", m.nodeID)
			} else {
				return fmt.Errorf("failed to read legacy disk file: %w", err)
			}
		}
	} else {
		records = append(records, legacyRecords...)
	}

	segmentFiles, err := m.segmentFiles()
	if err != nil {
		return fmt.Errorf("failed to list segment files: %w", err)
	}
	for _, segmentFile := range segmentFiles {
		seriesID, sequence, ok := parseSegmentFilename(segmentFile)
		if !ok {
			continue
		}
		segmentRecords, err := NewStorageEngine(segmentFile).Read()
		if err != nil {
			return fmt.Errorf("failed to read segment %s: %w", segmentFile, err)
		}
		records = append(records, segmentRecords...)
		for _, record := range segmentRecords {
			seriesKey := record.DeviceID + "|" + record.MetricName
			if seriesSegmentID(seriesKey) != seriesID {
				return fmt.Errorf("segment %s contains record for unexpected series %s", segmentFile, seriesKey)
			}
			if m.nextSegmentSeq[seriesKey] <= sequence {
				m.nextSegmentSeq[seriesKey] = sequence + 1
			}
		}
	}

	if len(records) == 0 {
		log.Printf("[Storage-%s] No records found on disk", m.nodeID)
		return nil
	}

	sort.SliceStable(records, func(i, j int) bool {
		return records[i].Timestamp < records[j].Timestamp
	})

	for _, record := range records {
		key := record.DeviceID + "|" + record.MetricName
		newSample := sample{
			Timestamp: record.Timestamp,
			Value:     record.Value,
			Role:      "primary",
		}

		series, ok := m.data[key]
		if !ok {
			series = &Series{}
			m.data[key] = series
		}
		series.Insert(newSample)
	}

	log.Printf("[Storage-%s] Reloaded %d records from disk into %d keys", m.nodeID, len(records), len(m.data))
	return nil
}

func (m *UnifiedStorage) Close() error {
	m.mu.Lock()

	log.Printf("[Storage-%s] Closing storage, flushing remaining %d records", m.nodeID, len(m.batch))

	if len(m.batch) > 0 {
		m.flushBatchUnlocked()
	}

	m.mu.Unlock()

	if m.file != nil {
		return m.file.Close()
	}
	return nil
}
