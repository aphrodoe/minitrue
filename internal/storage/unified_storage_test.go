package storage

import (
	"testing"
)

func TestIdempotentInsert(t *testing.T) {
	chunk := &Chunk{}

	// Insert a sample
	s1 := sample{Timestamp: 100, Value: 10.0}
	chunk.Insert(s1)

	if chunk.Count != 1 || chunk.Sum != 10.0 {
		t.Errorf("Expected count=1, sum=10.0, got count=%d, sum=%f", chunk.Count, chunk.Sum)
	}

	// Insert exact same sample again
	chunk.Insert(s1)

	// Idempotency: Count and Sum should NOT change
	if chunk.Count != 1 || chunk.Sum != 10.0 {
		t.Errorf("Idempotency failed: expected count=1, sum=10.0, got count=%d, sum=%f", chunk.Count, chunk.Sum)
	}

	// Insert new sample
	s2 := sample{Timestamp: 101, Value: 20.0}
	chunk.Insert(s2)

	if chunk.Count != 2 || chunk.Sum != 30.0 {
		t.Errorf("Expected count=2, sum=30.0, got count=%d, sum=%f", chunk.Count, chunk.Sum)
	}

	// Insert duplicate for s2
	chunk.Insert(s2)

	if chunk.Count != 2 || chunk.Sum != 30.0 {
		t.Errorf("Idempotency failed: expected count=2, sum=30.0, got count=%d, sum=%f", chunk.Count, chunk.Sum)
	}
}
