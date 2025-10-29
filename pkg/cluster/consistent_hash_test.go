package cluster

import (
    "fmt"
    "math"
    "testing"
)

// TestConsistentHashRing_AddNode tests adding nodes to the ring
func TestConsistentHashRing_AddNode(t *testing.T) {
    ring := NewConsistentHashRing(10)
    
    // Add first node
    ring.AddNode("node-1")
    if ring.Size() != 1 {
        t.Errorf("Expected 1 node, got %d", ring.Size())
    }
    
    // Add second node
    ring.AddNode("node-2")
    if ring.Size() != 2 {
        t.Errorf("Expected 2 nodes, got %d", ring.Size())
    }
    
    // Add third node
    ring.AddNode("node-3")
    if ring.Size() != 3 {
        t.Errorf("Expected 3 nodes, got %d", ring.Size())
    }
    
    // Check virtual nodes created
    expectedVirtualNodes := 3 * 10 // 3 nodes * 10 virtual nodes each
    if len(ring.sortedHashes) != expectedVirtualNodes {
        t.Errorf("Expected %d virtual nodes, got %d", 
                 expectedVirtualNodes, len(ring.sortedHashes))
    }
    
    // Test adding duplicate node (should not increase count)
    ring.AddNode("node-1")
    if ring.Size() != 3 {
        t.Errorf("Adding duplicate node should not increase size, got %d", ring.Size())
    }
}

// TestConsistentHashRing_RemoveNode tests removing nodes from the ring
func TestConsistentHashRing_RemoveNode(t *testing.T) {
    ring := NewConsistentHashRing(10)
    
    // Add nodes
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    initialSize := ring.Size()
    initialVirtualNodes := len(ring.sortedHashes)
    
    // Remove node
    ring.RemoveNode("node-2")
    
    if ring.Size() != initialSize-1 {
        t.Errorf("Expected size %d after removal, got %d", initialSize-1, ring.Size())
    }
    
    expectedVirtualNodes := initialVirtualNodes - 10 // Removed 10 virtual nodes
    if len(ring.sortedHashes) != expectedVirtualNodes {
        t.Errorf("Expected %d virtual nodes after removal, got %d",
                 expectedVirtualNodes, len(ring.sortedHashes))
    }
    
    // Test removing non-existent node (should not cause error)
    ring.RemoveNode("node-999")
    if ring.Size() != 2 {
        t.Errorf("Removing non-existent node should not change size, got %d", ring.Size())
    }
}

// TestConsistentHashRing_GetNode tests finding the node for a key
func TestConsistentHashRing_GetNode(t *testing.T) {
    ring := NewConsistentHashRing(150)
    
    // Test with no nodes
    _, err := ring.GetNode("test-key")
    if err == nil {
        t.Error("Expected error when ring is empty")
    }
    
    // Add nodes
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    // Test that same key always maps to same node
    node1, err := ring.GetNode("device-001")
    if err != nil {
        t.Fatalf("GetNode failed: %v", err)
    }
    
    node2, err := ring.GetNode("device-001")
    if err != nil {
        t.Fatalf("GetNode failed: %v", err)
    }
    
    if node1 != node2 {
        t.Errorf("Same key should always map to same node: %s vs %s", node1, node2)
    }
    
    // Test that different keys can map to different nodes
    node3, _ := ring.GetNode("device-002")
    node4, _ := ring.GetNode("device-003")
    
    // At least one should be different (very high probability)
    allSame := (node1 == node3) && (node3 == node4)
    if allSame {
        t.Log("Warning: All test keys mapped to same node (unlikely but possible)")
    }
}

// TestConsistentHashRing_GetNodes tests getting multiple nodes for replication
func TestConsistentHashRing_GetNodes(t *testing.T) {
    ring := NewConsistentHashRing(150)
    
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    // Test getting 3 nodes
    nodes, err := ring.GetNodes("device-001", 3)
    if err != nil {
        t.Fatalf("GetNodes failed: %v", err)
    }
    
    if len(nodes) != 3 {
        t.Errorf("Expected 3 nodes, got %d", len(nodes))
    }
    
    // Check all nodes are unique
    unique := make(map[string]bool)
    for _, node := range nodes {
        if unique[node] {
            t.Errorf("Duplicate node in result: %s", node)
        }
        unique[node] = true
    }
    
    if len(unique) != 3 {
        t.Errorf("Expected 3 unique nodes, got %d", len(unique))
    }
    
    // Test requesting more nodes than available
    nodes, err = ring.GetNodes("device-001", 10)
    if err != nil {
        t.Fatalf("GetNodes failed: %v", err)
    }
    
    if len(nodes) != 3 {
        t.Errorf("Should return all available nodes (3), got %d", len(nodes))
    }
}

// TestConsistentHashRing_Distribution tests distribution quality
func TestConsistentHashRing_Distribution(t *testing.T) {
    ring := NewConsistentHashRing(150)
    
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    // Test with 10,000 keys
    numKeys := 10000
    distribution := make(map[string]int)
    
    for i := 0; i < numKeys; i++ {
        key := fmt.Sprintf("device-%d", i)
        node, err := ring.GetNode(key)
        if err != nil {
            t.Fatalf("GetNode failed: %v", err)
        }
        distribution[node]++
    }
    
    // Each node should have roughly 1/3 of the keys
    expectedPerNode := numKeys / 3
    tolerance := float64(expectedPerNode) * 0.2 // Allow 20% variance
    
    t.Logf("Distribution of %d keys:", numKeys)
    for node, count := range distribution {
        percentage := float64(count) / float64(numKeys) * 100
        t.Logf("%s: %d keys (%.2f%%)", node, count, percentage)
        
        diff := math.Abs(float64(count) - float64(expectedPerNode))
        if diff > tolerance {
            t.Errorf("Node %s has poor distribution: %d keys (expected ~%d ± %.0f)",
                     node, count, expectedPerNode, tolerance)
        }
    }
    
    // Calculate standard deviation
    mean := float64(numKeys) / 3.0
    variance := 0.0
    for _, count := range distribution {
        diff := float64(count) - mean
        variance += diff * diff
    }
    variance /= float64(len(distribution))
    stdDev := math.Sqrt(variance)
    
    t.Logf("Standard deviation: %.2f (lower is better)", stdDev)
    
    // Standard deviation should be reasonably low
    maxStdDev := float64(expectedPerNode) * 0.1 // 10% of expected
    if stdDev > maxStdDev {
        t.Errorf("Standard deviation too high: %.2f (max: %.2f)", stdDev, maxStdDev)
    }
}

// TestConsistentHashRing_Rebalancing tests data movement when nodes change
func TestConsistentHashRing_Rebalancing(t *testing.T) {
    ring := NewConsistentHashRing(150)
    
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    // Track initial key placements
    numKeys := 1000
    initialPlacement := make(map[string]string)
    
    for i := 0; i < numKeys; i++ {
        key := fmt.Sprintf("device-%d", i)
        node, _ := ring.GetNode(key)
        initialPlacement[key] = node
    }
    
    // Remove a node
    ring.RemoveNode("node-2")
    
    // Check how many keys moved
    moved := 0
    for key, oldNode := range initialPlacement {
        newNode, _ := ring.GetNode(key)
        if oldNode != newNode {
            moved++
        }
    }
    
    // With 3 nodes, removing 1 should move ~33% of keys
    expectedMoved := numKeys / 3
    tolerance := float64(expectedMoved) * 0.3 // Allow 30% variance
    
    t.Logf("Keys moved after removing node-2: %d out of %d (%.2f%%)",
           moved, numKeys, float64(moved)/float64(numKeys)*100)
    
    diff := math.Abs(float64(moved) - float64(expectedMoved))
    if diff > tolerance {
        t.Errorf("Too many/few keys moved: %d (expected ~%d ± %.0f)",
                 moved, expectedMoved, tolerance)
    }
    
    // Add the node back
    ring.AddNode("node-2")
    
    // Most keys should return to original placement
    returned := 0
    for key, originalNode := range initialPlacement {
        currentNode, _ := ring.GetNode(key)
        if currentNode == originalNode {
            returned++
        }
    }
    
    t.Logf("Keys returned to original placement: %d out of %d (%.2f%%)",
           returned, numKeys, float64(returned)/float64(numKeys)*100)
    
    // Most keys should return (allowing some variance due to hash collisions)
    if returned < numKeys*8/10 { // At least 80%
        t.Errorf("Too few keys returned to original placement: %d (expected > %d)",
                 returned, numKeys*8/10)
    }
}

// TestConsistentHashRing_VirtualNodes tests virtual nodes configuration
func TestConsistentHashRing_VirtualNodes(t *testing.T) {
    testCases := []struct {
        virtualNodes int
        expected     int
    }{
        {1, 1},
        {10, 10},
        {50, 50},
        {150, 150},
        {0, 150},   // Should use default
        {-5, 150},  // Should use default
    }
    
    for _, tc := range testCases {
        t.Run(fmt.Sprintf("VirtualNodes=%d", tc.virtualNodes), func(t *testing.T) {
            ring := NewConsistentHashRing(tc.virtualNodes)
            ring.AddNode("node-1")
            
            actualVirtual := len(ring.sortedHashes)
            if actualVirtual != tc.expected {
                t.Errorf("Expected %d virtual nodes, got %d", tc.expected, actualVirtual)
            }
        })
    }
}

// TestConsistentHashRing_GetAllNodes tests retrieving all nodes
func TestConsistentHashRing_GetAllNodes(t *testing.T) {
    ring := NewConsistentHashRing(10)
    
    // Empty ring
    nodes := ring.GetAllNodes()
    if len(nodes) != 0 {
        t.Errorf("Expected 0 nodes in empty ring, got %d", len(nodes))
    }
    
    // Add nodes
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    nodes = ring.GetAllNodes()
    if len(nodes) != 3 {
        t.Errorf("Expected 3 nodes, got %d", len(nodes))
    }
    
    // Check all expected nodes are present
    expectedNodes := map[string]bool{
        "node-1": true,
        "node-2": true,
        "node-3": true,
    }
    
    for _, node := range nodes {
        if !expectedNodes[node] {
            t.Errorf("Unexpected node: %s", node)
        }
        delete(expectedNodes, node)
    }
    
    if len(expectedNodes) > 0 {
        t.Errorf("Missing nodes: %v", expectedNodes)
    }
}

// TestConsistentHashRing_Concurrent tests thread safety
func TestConsistentHashRing_Concurrent(t *testing.T) {
    ring := NewConsistentHashRing(150)
    
    // Pre-populate with some nodes
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    // Run concurrent operations
    done := make(chan bool, 3)
    
    // Goroutine 1: Keep adding and removing nodes
    go func() {
        for i := 0; i < 100; i++ {
            ring.AddNode(fmt.Sprintf("temp-node-%d", i))
            ring.RemoveNode(fmt.Sprintf("temp-node-%d", i))
        }
        done <- true
    }()
    
    // Goroutine 2: Keep querying
    go func() {
        for i := 0; i < 1000; i++ {
            ring.GetNode(fmt.Sprintf("device-%d", i))
        }
        done <- true
    }()
    
    // Goroutine 3: Keep getting multiple nodes
    go func() {
        for i := 0; i < 1000; i++ {
            ring.GetNodes(fmt.Sprintf("device-%d", i), 3)
        }
        done <- true
    }()
    
    // Wait for all goroutines
    <-done
    <-done
    <-done
    
    // If we reach here without deadlock or panic, test passes
    t.Log("Concurrent operations completed successfully")
}

// TestConsistentHashRing_EdgeCases tests edge cases
func TestConsistentHashRing_EdgeCases(t *testing.T) {
    t.Run("Empty ring GetNode", func(t *testing.T) {
        ring := NewConsistentHashRing(10)
        _, err := ring.GetNode("test")
        if err == nil {
            t.Error("Expected error for empty ring")
        }
    })
    
    t.Run("Empty ring GetNodes", func(t *testing.T) {
        ring := NewConsistentHashRing(10)
        _, err := ring.GetNodes("test", 3)
        if err == nil {
            t.Error("Expected error for empty ring")
        }
    })
    
    t.Run("Single node", func(t *testing.T) {
        ring := NewConsistentHashRing(10)
        ring.AddNode("only-node")
        
        node, err := ring.GetNode("test")
        if err != nil {
            t.Fatalf("Error: %v", err)
        }
        if node != "only-node" {
            t.Errorf("Expected 'only-node', got '%s'", node)
        }
    })
    
    t.Run("GetNodes with count 0", func(t *testing.T) {
        ring := NewConsistentHashRing(10)
        ring.AddNode("node-1")
        
        nodes, err := ring.GetNodes("test", 0)
        if err != nil {
            t.Fatalf("Error: %v", err)
        }
        if len(nodes) != 0 {
            t.Errorf("Expected 0 nodes, got %d", len(nodes))
        }
    })
}

// BenchmarkConsistentHashRing_AddNode benchmarks adding nodes
func BenchmarkConsistentHashRing_AddNode(b *testing.B) {
    ring := NewConsistentHashRing(150)
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ring.AddNode(fmt.Sprintf("node-%d", i))
    }
}

// BenchmarkConsistentHashRing_GetNode benchmarks finding a node
func BenchmarkConsistentHashRing_GetNode(b *testing.B) {
    ring := NewConsistentHashRing(150)
    
    // Pre-populate
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ring.GetNode(fmt.Sprintf("device-%d", i))
    }
}

// BenchmarkConsistentHashRing_GetNodes benchmarks getting multiple nodes
func BenchmarkConsistentHashRing_GetNodes(b *testing.B) {
    ring := NewConsistentHashRing(150)
    
    // Pre-populate
    ring.AddNode("node-1")
    ring.AddNode("node-2")
    ring.AddNode("node-3")
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ring.GetNodes(fmt.Sprintf("device-%d", i), 3)
    }
}

// BenchmarkConsistentHashRing_RemoveNode benchmarks removing nodes
func BenchmarkConsistentHashRing_RemoveNode(b *testing.B) {
    ring := NewConsistentHashRing(150)
    
    // Pre-populate with many nodes
    for i := 0; i < 1000; i++ {
        ring.AddNode(fmt.Sprintf("node-%d", i))
    }
    
    b.ResetTimer()
    for i := 0; i < b.N && i < 1000; i++ {
        ring.RemoveNode(fmt.Sprintf("node-%d", i))
    }
}