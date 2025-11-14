import React from 'react';
import './QueryResults.css';

const QueryResults = ({ result }) => {
  if (!result) return null;

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

  // Helper function to format operation name
  const formatOperation = (operation) => {
    const operationMap = {
      'avg': 'Average',
      'sum': 'Sum',
      'max': 'Maximum',
      'min': 'Minimum'
    };
    return operationMap[operation.toLowerCase()] || operation;
  };

  const formatDuration = (nanoseconds) => {
    // Backend now returns nanoseconds directly
    if (!nanoseconds || nanoseconds === 0) {
      return (
        <>
          0 <span className="duration-unit">nanoseconds</span>
        </>
      );
    }
    
    // Format with commas for readability on large numbers
    return (
      <>
        {nanoseconds.toLocaleString()} <span className="duration-unit">nanoseconds</span>
      </>
    );
  };

  return (
    <div className="query-results">
      <h2>Query Results</h2>
      
      <div className="results-grid">
        <div className="result-card primary">
          <div className="result-label">Result</div>
          <div className="result-value">{result.result.toFixed(4)}</div>
          <div className="result-operation">{formatOperation(result.operation)}</div>
        </div>

        <div className="result-card">
          <div className="result-label">Data Points</div>
          <div className="result-value">{result.count}</div>
        </div>

        <div className="result-card">
          <div className="result-label">Query Time</div>
          <div className="result-value">{formatDuration(result.duration_ns)}</div>
        </div>
      </div>

      <div className="results-details">
        <div className="detail-row">
          <span className="detail-label">Device ID:</span>
          <span className="detail-value">{formatDeviceId(result.device_id)}</span>
        </div>
        <div className="detail-row">
          <span className="detail-label">Metric:</span>
          <span className="detail-value">{formatMetricName(result.metric_name)}</span>
        </div>
        <div className="detail-row">
          <span className="detail-label">Operation:</span>
          <span className="detail-value">{formatOperation(result.operation)}</span>
        </div>
      </div>
    </div>
  );
};

export default QueryResults;

