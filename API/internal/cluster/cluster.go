package cluster

// GetPrimaryNode returns the node id (string) that is the PRIMARY for a given deviceID.
// -------------------------------------------------------------------------------
// IMPORTANT:
// Replace the body of GetPrimaryNode(...) with the real consistent-hashing call
// implemented by your teammate. For now this function returns a deterministic
// fallback primary ("ing1") so the project runs in single-node or multi-node tests.
//
// Example replacement (pseudo):
//    return consistenthash.FindPrimary(deviceID)
// -------------------------------------------------------------------------------
func GetPrimaryNode(deviceID string) string {
    // TODO: replace with the real consistent hashing function
    // For demo/fallback, return "ing1" (common primary). Update to actual logic later.
    return "ing1"
}
