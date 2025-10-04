package cluster

import "sync"

var (
	hashRing *ConsistentHashRing
	hooks    []func()
	hooksMu  sync.RWMutex
)

func init() {
	hashRing = NewConsistentHashRing(150)
}

func RegisterMembershipChangeHook(hook func()) {
	hooksMu.Lock()
	defer hooksMu.Unlock()
	hooks = append(hooks, hook)
}



func GetNodesForKey(key string, replicationFactor int) []string {
	nodes, err := hashRing.GetNodes(key, replicationFactor)
	if err != nil || len(nodes) == 0 {
		return nil
	}
	return nodes
}

func GetHashRing() *ConsistentHashRing {
	return hashRing
}

func SetHashRing(ring *ConsistentHashRing) {
	hashRing = ring
}


