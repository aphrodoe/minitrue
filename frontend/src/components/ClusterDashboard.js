import React, { useState, useEffect } from 'react';
import { fetchFromCluster } from '../clusterClient';
import './ClusterDashboard.css';

const ClusterDashboard = () => {
  const [clusterState, setClusterState] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  const fetchClusterHealth = async () => {
    try {
      const response = await fetchFromCluster('/cluster/members', { method: 'GET' });
      const data = await response.json();
      setClusterState(data);
      setError(null);
    } catch (err) {
      console.error('Failed to fetch cluster health:', err);
      setError('Unable to fetch cluster topology. The cluster might be down.');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    fetchClusterHealth();
    const interval = setInterval(fetchClusterHealth, 5000);
    return () => clearInterval(interval);
  }, []);

  if (loading) {
    return <div className="cluster-dashboard loading">Loading cluster topology...</div>;
  }

  if (error && !clusterState) {
    return <div className="cluster-dashboard error">{error}</div>;
  }

  const nodes = clusterState?.nodes ? Object.values(clusterState.nodes) : [];

  return (
    <div className="cluster-dashboard">
      <div className="dashboard-header">
        <h2>Cluster Topology</h2>
        <div className="cluster-meta">
          <span className="meta-badge">Replication Factor: {clusterState?.replication_factor || 2}</span>
          <span className="meta-badge">Ring Version: {clusterState?.version || 0}</span>
          <span className="meta-badge">Total Nodes: {nodes.length}</span>
        </div>
      </div>

      <div className="node-grid">
        {nodes.length === 0 ? (
          <div className="no-nodes">No nodes actively participating in the cluster.</div>
        ) : (
          nodes.map((node) => {
            const isAlive = node.status === 'alive';
            const statusClass = isAlive ? 'status-alive' : 'status-dead';
            
            return (
              <div key={node.id} className={`node-card ${statusClass}`}>
                <div className="node-header">
                  <h3>{node.id}</h3>
                  <span className={`status-indicator ${statusClass}`}></span>
                </div>
                <div className="node-body">
                  <div className="info-row">
                    <span className="label">Address:</span>
                    <span className="value">{node.address}</span>
                  </div>
                  <div className="info-row">
                    <span className="label">HTTP Port:</span>
                    <span className="value">{node.http_port}</span>
                  </div>
                  <div className="info-row">
                    <span className="label">Status:</span>
                    <span className={`value ${statusClass}`}>{node.status.toUpperCase()}</span>
                  </div>
                  <div className="info-row">
                    <span className="label">Last Heartbeat:</span>
                    <span className="value">
                      {new Date(node.last_heartbeat).toLocaleTimeString()}
                    </span>
                  </div>
                </div>
              </div>
            );
          })
        )}
      </div>
    </div>
  );
};

export default ClusterDashboard;
