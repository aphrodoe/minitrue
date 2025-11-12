import React, { useState, useEffect, useRef } from 'react';
import './RealTimeMonitor.css';

const RealTimeMonitor = () => {
  const [dataPoints, setDataPoints] = useState([]);
  const [isConnected, setIsConnected] = useState(false);
  const [stats, setStats] = useState({
    totalMessages: 0,
    messagesPerSecond: 0,
    connectedClients: 0,
  });
  const [filter, setFilter] = useState({
    deviceId: '',
    metricName: '',
  });

  const wsRef = useRef(null);
  const messageCountRef = useRef(0);
  const lastSecondRef = useRef(Date.now());
  const reconnectTimeoutRef = useRef(null);

  useEffect(() => {
    connectWebSocket();

    return () => {
      if (wsRef.current) {
        wsRef.current.close();
      }
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current);
      }
    };
  }, []);

  const connectWebSocket = () => {
    const ws = new WebSocket('ws://localhost:8080/ws');

    ws.onopen = () => {
      console.log('WebSocket connected');
      setIsConnected(true);
      fetchStats();
    };

    ws.onmessage = (event) => {
      try {
        const lines = event.data.trim().split('\n');
        const newDataPoints = lines.map(line => JSON.parse(line));
        
        setDataPoints(prev => {
          const updated = [...newDataPoints, ...prev].slice(0, 100); // Keep last 100 points
          return updated;
        });

        // Update stats
        messageCountRef.current += newDataPoints.length;
        const now = Date.now();
        if (now - lastSecondRef.current >= 1000) {
          setStats(prev => ({
            ...prev,
            totalMessages: prev.totalMessages + messageCountRef.current,
            messagesPerSecond: messageCountRef.current,
          }));
          messageCountRef.current = 0;
          lastSecondRef.current = now;
        }
      } catch (err) {
        console.error('Error parsing message:', err);
      }
    };

    ws.onerror = (error) => {
      console.error('WebSocket error:', error);
    };

    ws.onclose = () => {
      console.log('WebSocket disconnected');
      setIsConnected(false);
      
      // Attempt to reconnect after 3 seconds
      reconnectTimeoutRef.current = setTimeout(() => {
        console.log('Attempting to reconnect...');
        connectWebSocket();
      }, 3000);
    };

    wsRef.current = ws;
  };

  const fetchStats = async () => {
    try {
      const response = await fetch('http://localhost:8080/ws/stats');
      const data = await response.json();
      setStats(prev => ({
        ...prev,
        connectedClients: data.connected_clients,
      }));
    } catch (err) {
      console.error('Error fetching stats:', err);
    }
  };

  const filteredDataPoints = dataPoints.filter(dp => {
    if (filter.deviceId && !dp.device_id.toLowerCase().includes(filter.deviceId.toLowerCase())) {
      return false;
    }
    if (filter.metricName && !dp.metric_name.toLowerCase().includes(filter.metricName.toLowerCase())) {
      return false;
    }
    return true;
  });

  const formatTimestamp = (timestamp) => {
    const date = new Date(timestamp * 1000);
    return date.toLocaleTimeString();
  };

  const formatReceivedTime = (receivedAt) => {
    const date = new Date(receivedAt);
    return date.toLocaleTimeString() + '.' + String(date.getMilliseconds()).padStart(3, '0');
  };

  const getDeviceColor = (deviceId) => {
    const colors = {
      'sensor_1': '#4CAF50',
      'sensor_2': '#2196F3',
      'sensor_3': '#FF9800',
    };
    return colors[deviceId] || '#9E9E9E';
  };

  return (
    <div className="realtime-monitor">
      <div className="monitor-header">
        <h2>Real-Time Data Monitor</h2>
        <div className="connection-status">
          <span className={`status-indicator ${isConnected ? 'connected' : 'disconnected'}`}>
            {isConnected ? '● Connected' : '○ Disconnected'}
          </span>
        </div>
      </div>

      <div className="stats-grid">
        <div className="stat-card">
          <div className="stat-label">Total Messages</div>
          <div className="stat-value">{stats.totalMessages.toLocaleString()}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Messages/Second</div>
          <div className="stat-value">{stats.messagesPerSecond}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Connected Clients</div>
          <div className="stat-value">{stats.connectedClients}</div>
        </div>
        <div className="stat-card">
          <div className="stat-label">Displaying</div>
          <div className="stat-value">{filteredDataPoints.length}</div>
        </div>
      </div>

      <div className="filters">
        <input
          type="text"
          placeholder="Filter by Device ID..."
          value={filter.deviceId}
          onChange={(e) => setFilter(prev => ({ ...prev, deviceId: e.target.value }))}
          className="filter-input"
        />
        <input
          type="text"
          placeholder="Filter by Metric..."
          value={filter.metricName}
          onChange={(e) => setFilter(prev => ({ ...prev, metricName: e.target.value }))}
          className="filter-input"
        />
        <button
          onClick={() => setFilter({ deviceId: '', metricName: '' })}
          className="clear-filter-btn"
        >
          Clear Filters
        </button>
      </div>

      <div className="data-stream">
        {filteredDataPoints.length === 0 ? (
          <div className="no-data">
            {isConnected ? 'Waiting for data...' : 'Connecting to data stream...'}
          </div>
        ) : (
          <div className="data-list">
            {filteredDataPoints.map((dp, index) => (
              <div key={`${dp.device_id}-${dp.timestamp}-${index}`} className="data-item">
                <div className="data-item-header">
                  <span
                    className="device-badge"
                    style={{ backgroundColor: getDeviceColor(dp.device_id) }}
                  >
                    {dp.device_id}
                  </span>
                  <span className="metric-name">{dp.metric_name}</span>
                  <span className="received-time">{formatReceivedTime(dp.received_at)}</span>
                </div>
                <div className="data-item-body">
                  <div className="data-field">
                    <span className="field-label">Value:</span>
                    <span className="field-value value-highlight">{dp.value.toFixed(2)}</span>
                  </div>
                  <div className="data-field">
                    <span className="field-label">Timestamp:</span>
                    <span className="field-value">{formatTimestamp(dp.timestamp)}</span>
                  </div>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
};

export default RealTimeMonitor;