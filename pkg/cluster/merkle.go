package cluster

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "sort"
)

// MerkleTree represents a Merkle tree for data verification
type MerkleTree struct {
    Root *MerkleNode
}

// MerkleNode represents a node in the Merkle tree
type MerkleNode struct {
    Hash  string
    Left  *MerkleNode
    Right *MerkleNode
    Data  string // Only set for leaf nodes
}

// NewMerkleTree creates a new Merkle tree from data
func NewMerkleTree(data []string) *MerkleTree {
    if len(data) == 0 {
        return &MerkleTree{Root: nil}
    }

    // Sort data for consistency
    sortedData := make([]string, len(data))
    copy(sortedData, data)
    sort.Strings(sortedData)

    // Create leaf nodes
    nodes := make([]*MerkleNode, len(sortedData))
    for i, d := range sortedData {
        hash := hashData(d)
        nodes[i] = &MerkleNode{
            Hash: hash,
            Data: d,
        }
    }

    // Build tree bottom-up
    root := buildTree(nodes)

    return &MerkleTree{Root: root}
}

// buildTree recursively builds the Merkle tree
func buildTree(nodes []*MerkleNode) *MerkleNode {
    if len(nodes) == 0 {
        return nil
    }

    if len(nodes) == 1 {
        return nodes[0]
    }

    var parentNodes []*MerkleNode

    // Pair up nodes and create parents
    // for i := 0; i < len(nodes); i += 2 {
    //     var left, right *MerkleNode
    //     left = nodes[i]

    //     if i+1 < len(nodes) {
    //         right = nodes[i+1]
    //     } else {
    //         // Odd number of nodes, duplicate the last one
    //         right = nodes[i]
    //     }

    //     parentHash := hashData(left.Hash + right.Hash)
    //     parent := &MerkleNode{
    //         Hash:  parentHash,
    //         Left:  left,
    //         Right: right,
    //     }

    //     parentNodes = append(parentNodes, parent)
    // }
    // Pair up nodes and create parents
for i := 0; i < len(nodes); i += 2 {
    var left, right *MerkleNode
    left = nodes[i]

    if i+1 < len(nodes) {
        right = nodes[i+1]
    } else {
        // Odd number of nodes, duplicate the last one
        right = nodes[i]
    }

    // Use sorted order for consistent hashing
    var combinedHash string
    if left.Hash < right.Hash {
        combinedHash = left.Hash + right.Hash
    } else {
        combinedHash = right.Hash + left.Hash
    }
    parentHash := hashData(combinedHash)
    
    parent := &MerkleNode{
        Hash:  parentHash,
        Left:  left,
        Right: right,
    }

    parentNodes = append(parentNodes, parent)
}

    return buildTree(parentNodes)
}

// GetRootHash returns the root hash of the tree
func (mt *MerkleTree) GetRootHash() string {
    if mt.Root == nil {
        return ""
    }
    return mt.Root.Hash
}

// GetProof generates a Merkle proof for a specific data item
func (mt *MerkleTree) GetProof(data string) ([]string, error) {
    if mt.Root == nil {
        return nil, fmt.Errorf("empty tree")
    }

    targetHash := hashData(data)
    proof := []string{}

    if !findProof(mt.Root, targetHash, &proof) {
        return nil, fmt.Errorf("data not found in tree")
    }

    return proof, nil
}

// findProof recursively finds the proof path
func findProof(node *MerkleNode, targetHash string, proof *[]string) bool {
    if node == nil {
        return false
    }

    // Leaf node
    if node.Left == nil && node.Right == nil {
        return node.Hash == targetHash
    }

    // Search left subtree
    if findProof(node.Left, targetHash, proof) {
        if node.Right != nil {
            *proof = append(*proof, node.Right.Hash)
        }
        return true
    }

    // Search right subtree
    if findProof(node.Right, targetHash, proof) {
        if node.Left != nil {
            *proof = append(*proof, node.Left.Hash)
        }
        return true
    }

    return false
}

// VerifyProof verifies a Merkle proof
func VerifyProof(rootHash string, data string, proof []string) bool {
    currentHash := hashData(data)

    for _, siblingHash := range proof {
        // Combine hashes in sorted order for consistency
        if currentHash < siblingHash {
            currentHash = hashData(currentHash + siblingHash)
        } else {
            currentHash = hashData(siblingHash + currentHash)
        }
    }

    return currentHash == rootHash
}

// CompareTrees compares two Merkle trees and returns differences
func CompareTrees(tree1, tree2 *MerkleTree) []string {
    if tree1.Root == nil && tree2.Root == nil {
        return []string{}
    }

    if tree1.Root == nil || tree2.Root == nil {
        return []string{"trees are fundamentally different"}
    }

    if tree1.GetRootHash() == tree2.GetRootHash() {
        return []string{} // Trees are identical
    }

    differences := []string{}
    compareDFS(tree1.Root, tree2.Root, &differences)

    return differences
}

// compareDFS performs depth-first search to find differences
func compareDFS(node1, node2 *MerkleNode, differences *[]string) {
    if node1 == nil && node2 == nil {
        return
    }

    if node1 == nil || node2 == nil {
        *differences = append(*differences, "structure mismatch")
        return
    }

    if node1.Hash != node2.Hash {
        // Leaf nodes - record the difference
        if node1.Left == nil && node1.Right == nil {
            *differences = append(*differences, fmt.Sprintf("leaf mismatch: %s vs %s", 
                                                           node1.Data, node2.Data))
            return
        }

        // Internal nodes - recurse
        compareDFS(node1.Left, node2.Left, differences)
        compareDFS(node1.Right, node2.Right, differences)
    }
}

// hashData creates a SHA-256 hash of the input
func hashData(data string) string {
    hash := sha256.Sum256([]byte(data))
    return hex.EncodeToString(hash[:])
}

// GetAllLeafData returns all leaf data from the tree
func (mt *MerkleTree) GetAllLeafData() []string {
    if mt.Root == nil {
        return []string{}
    }

    var leaves []string
    collectLeaves(mt.Root, &leaves)
    return leaves
}

// collectLeaves recursively collects all leaf data
func collectLeaves(node *MerkleNode, leaves *[]string) {
    if node == nil {
        return
    }

    // Leaf node
    if node.Left == nil && node.Right == nil {
        *leaves = append(*leaves, node.Data)
        return
    }

    collectLeaves(node.Left, leaves)
    collectLeaves(node.Right, leaves)
}

// MerkleSync represents a synchronization state between two nodes
type MerkleSync struct {
    LocalTree   *MerkleTree
    RemoteHash  string
    Differences []string
}

// NewMerkleSync creates a new Merkle sync instance
func NewMerkleSync(localData []string, remoteHash string) *MerkleSync {
    return &MerkleSync{
        LocalTree:   NewMerkleTree(localData),
        RemoteHash:  remoteHash,
        Differences: []string{},
    }
}

// NeedSync checks if synchronization is needed
func (ms *MerkleSync) NeedSync() bool {
    return ms.LocalTree.GetRootHash() != ms.RemoteHash
}

// GenerateSyncPlan creates a plan for synchronization
func (ms *MerkleSync) GenerateSyncPlan(remoteTree *MerkleTree) []string {
    return CompareTrees(ms.LocalTree, remoteTree)
}