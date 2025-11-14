import React, { useState, useEffect, useRef } from 'react';
import { CSSTransition, TransitionGroup } from 'react-transition-group';
import './RealTimeMonitor.css';

const RealTimeMonitor = () => {
  // Helper function to format device ID aesthetically
  const formatDeviceId = (deviceId) => {
    if (!deviceId) return '';
    // Convert "sensor_1" to "Sensor 1", "sensor_2" to "Sensor 2", etc.
    return deviceId
      .split('_')
      .map(word => word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ');
  };

  // Helper function to format metric name aesthetically
  const formatMetricName = (metricName) => {
    if (!metricName) return '';
    // Convert "temperature" to "Temperature"
    return metricName.charAt(0).toUpperCase() + metricName.slice(1);
  };

  const [dataPoints, setDataPoints] = useState([]);
  const [isConnected, setIsConnected] = useState(false);
  const [stats, setStats] = useState({
    totalMessages: 0,
    messagesPerSecond: 0,
  });
  const [filter, setFilter] = useState({
    deviceId: '',
    metricName: '',
  });
  const [showGraph, setShowGraph] = useState(false);
  const [selectedSensor, setSelectedSensor] = useState('all'); 
  const [canvasSize, setCanvasSize] = useState({ width: 800, height: 400 });
  
  const [graphAverage, setGraphAverage] = useState(0);

  const wsRef = useRef(null);
  const messageCountRef = useRef(0);
  const lastSecondRef = useRef(Date.now());
  const reconnectTimeoutRef = useRef(null);
  const canvasRef = useRef(null);

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
    };

    ws.onmessage = (event) => {
      try {
        const lines = event.data.trim().split('\n');
        const newDataPoints = lines.map(line => JSON.parse(line));
        
        setDataPoints(prev => {
          const updated = [...newDataPoints, ...prev].slice(0, 100); 
          return updated;
        });

        messageCountRef.current += newDataPoints.length;
        
        setStats(prev => ({
          ...prev,
          totalMessages: prev.totalMessages + newDataPoints.length,
        }));
        
        const now = Date.now();
        if (now - lastSecondRef.current >= 1000) {
          setStats(prev => ({
            ...prev,
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
      
      reconnectTimeoutRef.current = setTimeout(() => {
        console.log('Attempting to reconnect...');
        connectWebSocket();
      }, 3000);
    };

    wsRef.current = ws;
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

  const temperatureDataPoints = filteredDataPoints.filter(dp => 
    dp.metric_name.toLowerCase() === 'temperature'
  );

  const graphDataPoints = selectedSensor === 'all' 
    ? temperatureDataPoints 
    : temperatureDataPoints.filter(dp => dp.device_id === selectedSensor);

  useEffect(() => {
    if (!showGraph || !canvasRef.current) {
      const canvas = canvasRef.current;
      if (canvas) {
        const ctx = canvas.getContext('2d');
        ctx.clearRect(0, 0, canvas.width, canvas.height);
        ctx.fillStyle = 'rgba(0, 0, 0, 0.3)';
        ctx.fillRect(0, 0, canvas.width, canvas.height);
      }
      if (graphDataPoints.length === 0) return;
    }
    
    if (!canvasRef.current) return;

    const canvas = canvasRef.current;
    const ctx = canvas.getContext('2d');
    const width = canvas.width;
    const height = canvas.height;
    const padding = { top: 40, right: 40, bottom: 60, left: 80 };

    ctx.clearRect(0, 0, width, height);
    ctx.fillStyle = 'rgba(0, 0, 0, 0.3)';
    ctx.fillRect(0, 0, width, height);

    const sortedData = [...graphDataPoints].sort((a, b) => a.timestamp - b.timestamp);

    if (sortedData.length === 0) {
      setGraphAverage(0);
      return;
    }

    const timestamps = sortedData.map(dp => dp.timestamp);
    const temperatures = sortedData.map(dp => dp.value);

    const sum = temperatures.reduce((acc, val) => acc + val, 0);
    const avg = temperatures.length > 0 ? sum / temperatures.length : 0;
    setGraphAverage(avg);

    const minTime = Math.min(...timestamps);
    const maxTime = Math.max(...timestamps);
    const minTemp = Math.min(...temperatures);
    const maxTemp = Math.max(...temperatures);
    const tempRange = maxTemp - minTemp || 1;
    const timeRange = maxTime - minTime || 1;

    ctx.strokeStyle = 'rgba(255, 255, 255, 0.3)';
    ctx.lineWidth = 1;

    ctx.beginPath();
    ctx.moveTo(padding.left, height - padding.bottom);
    ctx.lineTo(width - padding.right, height - padding.bottom);
    ctx.stroke();

    ctx.beginPath();
    ctx.moveTo(padding.left, padding.top);
    ctx.lineTo(padding.left, height - padding.bottom);
    ctx.stroke();

    ctx.strokeStyle = 'rgba(255, 255, 255, 0.1)';
    ctx.lineWidth = 1;

    for (let i = 0; i <= 5; i++) {
      const y = padding.top + (height - padding.top - padding.bottom) * (i / 5);
      ctx.beginPath();
      ctx.moveTo(padding.left, y);
      ctx.lineTo(width - padding.right, y);
      ctx.stroke();
    }

    for (let i = 0; i <= 5; i++) {
      const x = padding.left + (width - padding.left - padding.right) * (i / 5);
      ctx.beginPath();
      ctx.moveTo(x, padding.top);
      ctx.lineTo(x, height - padding.bottom);
      ctx.stroke();
    }

    ctx.fillStyle = 'rgba(255, 255, 255, 0.7)';
    ctx.font = '12px Arial';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'top';

    for (let i = 0; i <= 5; i++) {
      const timeValue = minTime + (timeRange * i / 5);
      const x = padding.left + (width - padding.left - padding.right) * (i / 5);
      const date = new Date(timeValue * 1000);
      const timeStr = date.toLocaleTimeString();
      ctx.fillText(timeStr, x, height - padding.bottom + 10);
    }

    ctx.textAlign = 'right';
    ctx.textBaseline = 'middle';
    for (let i = 0; i <= 5; i++) {
      const tempValue = minTemp + (tempRange * i / 5);
      const y = padding.top + (height - padding.top - padding.bottom) * (1 - i / 5);
      ctx.fillText(tempValue.toFixed(1) + '°', padding.left - 15, y);
    }

    ctx.fillStyle = 'rgba(255, 255, 255, 0.9)';
    ctx.font = 'bold 14px Arial';
    ctx.textAlign = 'center';
    ctx.fillText('Time', width / 2, height - padding.bottom + 35);
    
    ctx.save();
    ctx.translate(15, height / 2);
    ctx.rotate(-Math.PI / 2);
    ctx.fillText('Temperature (°C)', 0, 0);
    ctx.restore();

    const plotWidth = width - padding.left - padding.right;
    const plotHeight = height - padding.top - padding.bottom;

    const deviceGroups = {};
    sortedData.forEach(dp => {
      if (!deviceGroups[dp.device_id]) {
        deviceGroups[dp.device_id] = [];
      }
      deviceGroups[dp.device_id].push(dp);
    });

    Object.keys(deviceGroups).forEach(deviceId => {
      const deviceData = deviceGroups[deviceId];
      const color = getDeviceColor(deviceId);

      ctx.strokeStyle = color;
      ctx.lineWidth = 2;
      ctx.beginPath();
      
      deviceData.forEach((dp, index) => {
        const x = padding.left + ((dp.timestamp - minTime) / timeRange) * plotWidth;
        const y = padding.top + plotHeight - ((dp.value - minTemp) / tempRange) * plotHeight;
        
        if (index === 0) {
          ctx.moveTo(x, y);
        } else {
          ctx.lineTo(x, y);
        }
      });
      ctx.stroke();

      ctx.fillStyle = color;
      deviceData.forEach(dp => {
        const x = padding.left + ((dp.timestamp - minTime) / timeRange) * plotWidth;
        const y = padding.top + plotHeight - ((dp.value - minTemp) / tempRange) * plotHeight;
        
        ctx.beginPath();
        ctx.arc(x, y, 4, 0, 2 * Math.PI);
        ctx.fill();
      });
    });

    const legendY = padding.top - 25;
    let legendX = padding.left;
    ctx.font = '12px Arial';
    ctx.textAlign = 'left';
    ctx.textBaseline = 'middle';
    
    Object.keys(deviceGroups).forEach(deviceId => {
      const color = getDeviceColor(deviceId);
      ctx.fillStyle = color;
      ctx.beginPath();
      ctx.arc(legendX, legendY, 6, 0, 2 * Math.PI);
      ctx.fill();
      
      ctx.fillStyle = 'rgba(255, 255, 255, 0.9)';
      ctx.fillText(deviceId, legendX + 15, legendY);
      legendX += 120;
    });

  }, [showGraph, graphDataPoints, canvasSize, selectedSensor]);

  useEffect(() => {
    if (canvasRef.current && showGraph) {
      const canvas = canvasRef.current;
      const container = canvas.parentElement;
      if (container) {
        const containerWidth = container.clientWidth - 40;
        const width = containerWidth > 0 ? containerWidth : 800;
        canvas.width = width;
        canvas.height = 400;
        setCanvasSize({ width, height: 400 });
      } else {
        canvas.width = 800;
        canvas.height = 400;
        setCanvasSize({ width: 800, height: 400 });
      }
    }
  }, [showGraph]);

  useEffect(() => {
    const handleResize = () => {
      if (canvasRef.current && showGraph) {
        const canvas = canvasRef.current;
        const container = canvas.parentElement;
        if (container) {
          const containerWidth = container.clientWidth - 40;
          const width = containerWidth > 0 ? containerWidth : 800;
          canvas.width = width;
          canvas.height = 400;
          setCanvasSize({ width, height: 400 });
        }
      }
    };

    window.addEventListener('resize', handleResize);
    return () => window.removeEventListener('resize', handleResize);
  }, [showGraph]);

  return (
    <div className="realtime-monitor">
      <div className="monitor-header">
        <h2>Real-Time Data Monitor</h2>
        <div className="header-controls">
          <button
            onClick={() => setShowGraph(!showGraph)}
            className="visualize-btn"
          >
            {showGraph ? 'Hide Graph' : 'Show Graph'}
          </button>
          <div className="connection-status">
            <span className={`status-indicator ${isConnected ? 'connected' : 'disconnected'}`}>
              {isConnected ? '● Connected' : '○ Disconnected'}
            </span>
          </div>
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
          <div className="stat-label">Displaying</div>
          <div className="stat-value">{filteredDataPoints.length}</div>
        </div>
      </div>

      {showGraph && (
        <div className="graph-container">
          <div className="sensor-selector">
            <button
              onClick={() => setSelectedSensor('all')}
              className={`sensor-btn ${selectedSensor === 'all' ? 'active' : ''}`}
            >
              All Sensors
            </button>
            <button
              onClick={() => setSelectedSensor('sensor_1')}
              className={`sensor-btn sensor-1 ${selectedSensor === 'sensor_1' ? 'active' : ''}`}
            >
              Sensor 1
            </button>
            <button
              onClick={() => setSelectedSensor('sensor_2')}
              className={`sensor-btn sensor-2 ${selectedSensor === 'sensor_2' ? 'active' : ''}`}
            >
              Sensor 2
            </button>
            <button
              onClick={() => setSelectedSensor('sensor_3')}
              className={`sensor-btn sensor-3 ${selectedSensor === 'sensor_3' ? 'active' : ''}`}
            >
              Sensor 3
            </button>
          </div>
          {graphDataPoints.length === 0 ? (
            <div className="no-data">
              No temperature data available for selected sensor. Please ensure temperature metrics are being received.
            </div>
          ) : (
            <>
              <canvas ref={canvasRef} className="temperature-graph" />
              <div 
                className="graph-average"
                style={{
                  textAlign: 'center',
                  marginTop: '10px',
                  fontSize: '1.1em',
                  fontWeight: 'bold',
                  color: 'rgba(255, 255, 255, 0.9)'
                }}
              >
                Window Average: {graphAverage.toFixed(2)}°C
              </div>
            </>
          )}
        </div>
      )}

      <div className="data-stream">
        {filteredDataPoints.length === 0 ? (
          <div className="no-data">
            {isConnected ? 'Waiting for data...' : 'Connecting to data stream...'}
          </div>
        ) : (
          <TransitionGroup className="data-list" component="div">
            {filteredDataPoints.map((dp) => (
              <CSSTransition
                key={dp.received_at}
                timeout={500}
                classNames="data-item-anim"
              >
                <div className="data-item">
                  <div className="data-item-header">
                    <span
                      className="device-badge"
                      style={{ backgroundColor: getDeviceColor(dp.device_id) }}
                    >
                      {formatDeviceId(dp.device_id)}
                    </span>
                    <span className="metric-name">{formatMetricName(dp.metric_name)}</span>
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
              </CSSTransition>
            ))}
          </TransitionGroup>
        )}
      </div>
    </div>
  );
};

export default RealTimeMonitor;