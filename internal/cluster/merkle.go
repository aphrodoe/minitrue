package cluster

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/minitrue/internal/models"
)

type MerkleNode struct {
	Left  *MerkleNode
	Right *MerkleNode
	Hash  string
	Data  *models.NodeInfo
}

type MerkleTree struct {
	Root *MerkleNode
}

func calculateHash(data string) string {
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

func hashNodeInfo(node *models.NodeInfo) string {
	// Hash the crucial state fields. LastHeartbeat is omitted because it updates
	// constantly and would cause the root hash to change every second even when
	// cluster membership is identical.
	data := fmt.Sprintf("%s|%s|%s|%d", node.ID, node.Address, node.Status, node.HTTPPort)
	return calculateHash(data)
}

// BuildMerkleTree constructs a Merkle Tree from the current cluster state.
func BuildMerkleTree(nodes map[string]*models.NodeInfo) *MerkleTree {
	if len(nodes) == 0 {
		return &MerkleTree{Root: &MerkleNode{Hash: calculateHash("empty")}}
	}

	var sortedNodes []*models.NodeInfo
	for _, n := range nodes {
		sortedNodes = append(sortedNodes, n)
	}
	
	// Deterministic sorting to ensure identical state yields identical tree
	sort.Slice(sortedNodes, func(i, j int) bool {
		return sortedNodes[i].ID < sortedNodes[j].ID
	})

	var leaves []*MerkleNode
	for _, n := range sortedNodes {
		leaves = append(leaves, &MerkleNode{
			Hash: hashNodeInfo(n),
			Data: n,
		})
	}

	root := buildTreeLevel(leaves)
	return &MerkleTree{Root: root}
}

func buildTreeLevel(nodes []*MerkleNode) *MerkleNode {
	if len(nodes) == 1 {
		return nodes[0]
	}

	var nextLevel []*MerkleNode
	for i := 0; i < len(nodes); i += 2 {
		if i+1 < len(nodes) {
			combinedHash := calculateHash(nodes[i].Hash + nodes[i+1].Hash)
			nextLevel = append(nextLevel, &MerkleNode{
				Left:  nodes[i],
				Right: nodes[i+1],
				Hash:  combinedHash,
			})
		} else {
			// If odd number of nodes, carry over the last one
			nextLevel = append(nextLevel, nodes[i])
		}
	}

	return buildTreeLevel(nextLevel)
}

func (m *MerkleTree) GetRootHash() string {
	if m.Root == nil {
		return ""
	}
	return m.Root.Hash
}
