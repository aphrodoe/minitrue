import React from 'react';
import './QueryResults.css';

const QueryResults = ({ result }) => {
  if (!result) return null;

  const formatDeviceId = (deviceId) => {
    if (!deviceId) return '';
    return deviceId
      .split('_')
      .map(word => word.charAt(0).toUpperCase() + word.slice(1))
      .join(' ');
  };

  const formatMetricName = (metricName) => {
    if (!metricName) return '';
    return metricName.charAt(0).toUpperCase() + metricName.slice(1);
  };

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
    if (!nanoseconds || nanoseconds === 0) {
      return (
        <>
          0 <span className="duration-unit">nanoseconds</span>
        </>
      );
    }
    
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

