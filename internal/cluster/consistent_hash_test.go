package cluster

import (
	"fmt"
	"math"
	"testing"
)

func TestConsistentHashRingDistributesSequentialSensorKeys(t *testing.T) {
	ring := NewConsistentHashRing(150)
	for i := 0; i < 10; i++ {
		ring.AddNode(fmt.Sprintf("node-%d", i))
	}

	counts := make(map[string]int)
	for i := 1; i <= 5000; i++ {
		key := fmt.Sprintf("sensor-%04d:temp", i)
		node, err := ring.GetNode(key)
		if err != nil {
			t.Fatalf("GetNode(%q) returned error: %v", key, err)
		}
		counts[node]++
	}

	mean := float64(5000) / float64(10)
	var squaredDiffs float64
	for i := 0; i < 10; i++ {
		nodeID := fmt.Sprintf("node-%d", i)
		diff := float64(counts[nodeID]) - mean
		squaredDiffs += diff * diff
	}
	stddev := math.Sqrt(squaredDiffs / float64(10))

	threshold := mean * 0.10
	if stddev > threshold {
		t.Fatalf("distribution is too uneven: counts=%v mean=%.2f stddev=%.2f threshold=%.2f", counts, mean, stddev, threshold)
	}
}

func TestConsistentHashRingGetNodesReturnsDistinctPreferenceList(t *testing.T) {
	ring := NewConsistentHashRing(150)
	ring.AddNode("node-a")
	ring.AddNode("node-b")
	ring.AddNode("node-c")

	nodes, err := ring.GetNodes("sensor-0001:temp", 2)
	if err != nil {
		t.Fatalf("GetNodes returned error: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected exactly 2 preference nodes, got %d: %v", len(nodes), nodes)
	}
	if nodes[0] == nodes[1] {
		t.Fatalf("expected distinct preference nodes, got %v", nodes)
	}
}

func TestConsistentHashRingLookupDeterministic(t *testing.T) {
	ring := NewConsistentHashRing(150)
	for i := 0; i < 5; i++ {
		ring.AddNode(fmt.Sprintf("node-%d", i))
	}

	const key = "sensor-0420:humidity"
	firstNode, err := ring.GetNode(key)
	if err != nil {
		t.Fatalf("first GetNode returned error: %v", err)
	}
	secondNode, err := ring.GetNode(key)
	if err != nil {
		t.Fatalf("second GetNode returned error: %v", err)
	}
	if firstNode != secondNode {
		t.Fatalf("GetNode is not deterministic: first=%q second=%q", firstNode, secondNode)
	}

	firstPreferenceList, err := ring.GetNodes(key, 2)
	if err != nil {
		t.Fatalf("first GetNodes returned error: %v", err)
	}
	secondPreferenceList, err := ring.GetNodes(key, 2)
	if err != nil {
		t.Fatalf("second GetNodes returned error: %v", err)
	}
	if len(firstPreferenceList) != len(secondPreferenceList) {
		t.Fatalf("GetNodes returned different lengths: first=%v second=%v", firstPreferenceList, secondPreferenceList)
	}
	for i := range firstPreferenceList {
		if firstPreferenceList[i] != secondPreferenceList[i] {
			t.Fatalf("GetNodes is not deterministic: first=%v second=%v", firstPreferenceList, secondPreferenceList)
		}
	}
}
