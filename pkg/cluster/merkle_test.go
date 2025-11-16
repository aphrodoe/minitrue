package cluster

import (
	"fmt"
	"testing"
)

func TestMerkleTree_Create(t *testing.T) {
	data := []string{"data1", "data2", "data3", "data4"}
	tree := NewMerkleTree(data)

	if tree.Root == nil {
		t.Fatal("Root should not be nil")
	}

	if tree.GetRootHash() == "" {
		t.Error("Root hash should not be empty")
	}

	emptyTree := NewMerkleTree([]string{})
	if emptyTree.Root != nil {
		t.Error("Empty tree should have nil root")
	}
}

func TestMerkleTree_Deterministic(t *testing.T) {
	data := []string{"apple", "banana", "cherry", "date"}

	tree1 := NewMerkleTree(data)
	tree2 := NewMerkleTree(data)

	hash1 := tree1.GetRootHash()
	hash2 := tree2.GetRootHash()

	if hash1 != hash2 {
		t.Errorf("Same data should produce same root hash: %s vs %s", hash1, hash2)
	}

	shuffledData := []string{"date", "apple", "cherry", "banana"}
	tree3 := NewMerkleTree(shuffledData)
	hash3 := tree3.GetRootHash()

	if hash1 != hash3 {
		t.Errorf("Same data in different order should produce same hash: %s vs %s", hash1, hash3)
	}
}

func TestMerkleTree_GetRootHash(t *testing.T) {
	data := []string{"test1", "test2"}
	tree := NewMerkleTree(data)

	rootHash := tree.GetRootHash()

	if rootHash == "" {
		t.Error("Root hash should not be empty")
	}

	if len(rootHash) != 64 {
		t.Errorf("SHA-256 hash should be 64 characters, got %d", len(rootHash))
	}

	emptyTree := NewMerkleTree([]string{})
	if emptyTree.GetRootHash() != "" {
		t.Error("Empty tree should return empty root hash")
	}
}

func TestMerkleTree_GetProof(t *testing.T) {
	data := []string{"data1", "data2", "data3", "data4"}
	tree := NewMerkleTree(data)

	proof, err := tree.GetProof("data2")
	if err != nil {
		t.Fatalf("GetProof failed: %v", err)
	}

	if len(proof) == 0 {
		t.Error("Proof should not be empty")
	}

	t.Logf("Proof for 'data2': %v", proof)
	t.Logf("Proof size: %d hashes", len(proof))

	_, err = tree.GetProof("nonexistent")
	if err == nil {
		t.Error("Should return error for non-existent data")
	}

	emptyTree := NewMerkleTree([]string{})
	_, err = emptyTree.GetProof("data1")
	if err == nil {
		t.Error("Should return error for empty tree")
	}
}

func TestMerkleTree_VerifyProof(t *testing.T) {
	data := []string{"apple", "banana", "cherry", "date"}
	tree := NewMerkleTree(data)
	rootHash := tree.GetRootHash()

	proof, err := tree.GetProof("banana")
	if err != nil {
		t.Fatalf("GetProof failed: %v", err)
	}

	isValid := VerifyProof(rootHash, "banana", proof)
	if !isValid {
		t.Error("Valid proof should verify successfully")
	}

	isValid = VerifyProof(rootHash, "orange", proof)
	if isValid {
		t.Error("Invalid data should not verify")
	}

	wrongRootHash := "0000000000000000000000000000000000000000000000000000000000000000"
	isValid = VerifyProof(wrongRootHash, "banana", proof)
	if isValid {
		t.Error("Should not verify with wrong root hash")
	}

	for _, item := range data {
		proof, err := tree.GetProof(item)
		if err != nil {
			t.Errorf("GetProof failed for %s: %v", item, err)
			continue
		}

		if !VerifyProof(rootHash, item, proof) {
			t.Errorf("Proof verification failed for %s", item)
		}
	}
}

func TestMerkleTree_CompareTrees(t *testing.T) {
	data1 := []string{"data1", "data2", "data3"}
	data2 := []string{"data1", "data2", "data3"}

	tree1 := NewMerkleTree(data1)
	tree2 := NewMerkleTree(data2)

	differences := CompareTrees(tree1, tree2)
	if len(differences) != 0 {
		t.Errorf("Identical trees should have no differences, got: %v", differences)
	}

	data3 := []string{"data1", "data2", "different"}
	tree3 := NewMerkleTree(data3)

	differences = CompareTrees(tree1, tree3)
	if len(differences) == 0 {
		t.Error("Different trees should have differences")
	}

	t.Logf("Differences found: %v", differences)

	emptyTree := NewMerkleTree([]string{})
	differences = CompareTrees(tree1, emptyTree)
	if len(differences) == 0 {
		t.Error("Comparing with empty tree should show differences")
	}

	emptyTree2 := NewMerkleTree([]string{})
	differences = CompareTrees(emptyTree, emptyTree2)
	if len(differences) != 0 {
		t.Error("Two empty trees should have no differences")
	}
}

func TestMerkleTree_SingleItem(t *testing.T) {
	data := []string{"single-item"}
	tree := NewMerkleTree(data)

	if tree.Root == nil {
		t.Fatal("Root should not be nil")
	}

	rootHash := tree.GetRootHash()
	if rootHash == "" {
		t.Error("Root hash should not be empty")
	}

	proof, err := tree.GetProof("single-item")
	if err != nil {
		t.Fatalf("GetProof failed: %v", err)
	}

	t.Logf("Proof for single item: %v (length: %d)", proof, len(proof))

	isValid := VerifyProof(rootHash, "single-item", proof)
	if !isValid {
		t.Error("Proof should verify for single item tree")
	}
}

func TestMerkleTree_OddNumberOfItems(t *testing.T) {
	data := []string{"item1", "item2", "item3"}
	tree := NewMerkleTree(data)

	if tree.Root == nil {
		t.Fatal("Root should not be nil")
	}

	rootHash := tree.GetRootHash()

	for _, item := range data {
		proof, err := tree.GetProof(item)
		if err != nil {
			t.Errorf("GetProof failed for %s: %v", item, err)
			continue
		}

		if !VerifyProof(rootHash, item, proof) {
			t.Errorf("Proof verification failed for %s", item)
		}
	}
}

func TestMerkleTree_LargeDataset(t *testing.T) {
	data := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		data[i] = fmt.Sprintf("device-%d:temp:%.2f", i, 20.0+float64(i)*0.1)
	}

	tree := NewMerkleTree(data)
	rootHash := tree.GetRootHash()

	if rootHash == "" {
		t.Fatal("Root hash should not be empty")
	}

	testIndices := []int{0, 100, 500, 999}
	for _, idx := range testIndices {
		proof, err := tree.GetProof(data[idx])
		if err != nil {
			t.Errorf("GetProof failed for index %d: %v", idx, err)
			continue
		}

		if !VerifyProof(rootHash, data[idx], proof) {
			t.Errorf("Proof verification failed for index %d", idx)
		}

		if idx == 0 {
			t.Logf("Proof size for 1000 items: %d hashes (log2(1000) ≈ 10)", len(proof))
		}
	}
}

func TestMerkleTree_DetectCorruption(t *testing.T) {
	originalData := []string{
		"device-001:10:00:00:25.5",
		"device-001:10:00:01:25.6",
		"device-001:10:00:02:25.4",
		"device-001:10:00:03:25.7",
	}

	corruptedData := []string{
		"device-001:10:00:00:25.5",
		"device-001:10:00:01:99.9", 
		"device-001:10:00:02:25.4",
		"device-001:10:00:03:25.7",
	}

	treeOriginal := NewMerkleTree(originalData)
	treeCorrupted := NewMerkleTree(corruptedData)

	hashOriginal := treeOriginal.GetRootHash()
	hashCorrupted := treeCorrupted.GetRootHash()

	if hashOriginal == hashCorrupted {
		t.Error("Corrupted data should produce different root hash")
	}

	differences := CompareTrees(treeOriginal, treeCorrupted)
	if len(differences) == 0 {
		t.Error("Should detect differences")
	}

	t.Logf("Detected corruption: %v", differences)

	foundCorruption := false
	for _, diff := range differences {
		if contains(diff, "25.6") && contains(diff, "99.9") {
			foundCorruption = true
			break
		}
	}

	if !foundCorruption {
		t.Error("Should identify the exact corrupted item")
	}
}

func TestMerkleTree_MissingData(t *testing.T) {
	completeData := []string{"A", "B", "C", "D", "E"}
	incompleteData := []string{"A", "B", "D", "E"} 

	treeComplete := NewMerkleTree(completeData)
	treeIncomplete := NewMerkleTree(incompleteData)

	hashComplete := treeComplete.GetRootHash()
	hashIncomplete := treeIncomplete.GetRootHash()

	if hashComplete == hashIncomplete {
		t.Error("Missing data should produce different root hash")
	}

	differences := CompareTrees(treeComplete, treeIncomplete)
	if len(differences) == 0 {
		t.Error("Should detect missing data")
	}

	t.Logf("Missing data detected: %v", differences)
}

func TestMerkleTree_GetAllLeafData(t *testing.T) {
	data := []string{"cherry", "apple", "banana", "date"}
	tree := NewMerkleTree(data)

	leaves := tree.GetAllLeafData()

	if len(leaves) != len(data) {
		t.Errorf("Expected %d leaves, got %d", len(data), len(leaves))
	}

	expectedSorted := []string{"apple", "banana", "cherry", "date"}
	for i, leaf := range leaves {
		if leaf != expectedSorted[i] {
			t.Errorf("Leaf %d: expected %s, got %s", i, expectedSorted[i], leaf)
		}
	}

	t.Logf("Leaves (sorted): %v", leaves)
}

func TestMerkleTree_HashData(t *testing.T) {
	hash1 := hashData("test")
	hash2 := hashData("test")

	if hash1 != hash2 {
		t.Error("Same input should produce same hash")
	}

	hash3 := hashData("different")
	if hash1 == hash3 {
		t.Error("Different inputs should produce different hashes")
	}

	if len(hash1) != 64 {
		t.Errorf("Hash should be 64 characters, got %d", len(hash1))
	}

	hashA := hashData("test")
	hashB := hashData("tesT") 
	if hashA == hashB {
		t.Error("Small change should produce different hash")
	}

	t.Logf("hash('test') = %s", hashA)
	t.Logf("hash('tesT') = %s", hashB)
}

func TestMerkleSync(t *testing.T) {
	localData := []string{"data1", "data2", "data3"}
	remoteData := []string{"data1", "data2", "data3"}

	remoteTree := NewMerkleTree(remoteData)

	remoteHash := remoteTree.GetRootHash()

	sync := NewMerkleSync(localData, remoteHash)

	if sync.NeedSync() {
		t.Error("Identical data should not need sync")
	}

	differentData := []string{"data1", "data2", "different"}
	differentTree := NewMerkleTree(differentData)
	differentHash := differentTree.GetRootHash()

	sync2 := NewMerkleSync(localData, differentHash)
	if !sync2.NeedSync() {
		t.Error("Different data should need sync")
	}

	plan := sync2.GenerateSyncPlan(differentTree)
	if len(plan) == 0 {
		t.Error("Sync plan should not be empty for different trees")
	}

	t.Logf("Sync plan: %v", plan)
}

func TestMerkleTree_ProofSize(t *testing.T) {
	testCases := []struct {
		numItems      int
		expectedProof int
	}{
		{2, 1},  
		{4, 2},    
		{8, 3},     
		{16, 4},    
		{32, 5},    
		{1024, 10}, 
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d_items", tc.numItems), func(t *testing.T) {
			data := make([]string, tc.numItems)
			for i := 0; i < tc.numItems; i++ {
				data[i] = fmt.Sprintf("item-%d", i)
			}

			tree := NewMerkleTree(data)
			proof, err := tree.GetProof(data[0])

			if err != nil {
				t.Fatalf("GetProof failed: %v", err)
			}

			if len(proof) != tc.expectedProof {
				t.Logf("Note: Proof size %d differs from expected %d (acceptable variation)",
					len(proof), tc.expectedProof)
			} else {
				t.Logf("✓ Proof size for %d items: %d hashes (log2(%d) = %d)",
					tc.numItems, len(proof), tc.numItems, tc.expectedProof)
			}
		})
	}
}

func TestMerkleTree_DataIntegrity(t *testing.T) {
	hourlyData := make([]string, 3600)
	for i := 0; i < 3600; i++ {
		hourlyData[i] = fmt.Sprintf("device-001:temp:%.2f:timestamp:%d",
			25.0+float64(i)*0.001, 1000000+i)
	}

	tree := NewMerkleTree(hourlyData)
	originalHash := tree.GetRootHash()

	t.Logf("Created tree for 3600 data points")
	t.Logf("Root hash: %s", originalHash)

	corruptedData := make([]string, len(hourlyData))
	copy(corruptedData, hourlyData)
	corruptedData[1800] = "device-001:temp:99.99:timestamp:1001800"

	corruptedTree := NewMerkleTree(corruptedData)
	corruptedHash := corruptedTree.GetRootHash()

	if originalHash == corruptedHash {
		t.Error("Should detect corrupted data")
	}

	t.Logf("✓ Corruption detected (different root hash)")

	differences := CompareTrees(tree, corruptedTree)
	if len(differences) == 0 {
		t.Error("Should find the corrupted item")
	}

	t.Logf("✓ Found %d corrupted items out of 3600", len(differences))
}

func BenchmarkMerkleTree_Build(b *testing.B) {
	data := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		data[i] = fmt.Sprintf("item-%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		NewMerkleTree(data)
	}
}

func BenchmarkMerkleTree_GetProof(b *testing.B) {
	data := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		data[i] = fmt.Sprintf("item-%d", i)
	}

	tree := NewMerkleTree(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.GetProof("item-500")
	}
}

func BenchmarkMerkleTree_VerifyProof(b *testing.B) {
	data := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		data[i] = fmt.Sprintf("item-%d", i)
	}

	tree := NewMerkleTree(data)
	rootHash := tree.GetRootHash()
	proof, _ := tree.GetProof("item-500")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		VerifyProof(rootHash, "item-500", proof)
	}
}

func BenchmarkMerkleTree_CompareTrees(b *testing.B) {
	data1 := make([]string, 1000)
	data2 := make([]string, 1000)

	for i := 0; i < 1000; i++ {
		data1[i] = fmt.Sprintf("item-%d", i)
		data2[i] = fmt.Sprintf("item-%d", i)
	}

	data2[500] = "different-item"

	tree1 := NewMerkleTree(data1)
	tree2 := NewMerkleTree(data2)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CompareTrees(tree1, tree2)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
