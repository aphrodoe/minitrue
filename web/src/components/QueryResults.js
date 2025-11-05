import React from 'react';
import './QueryResults.css';

const QueryResults = ({ result }) => {
  if (!result) return null;

  const formatTimestamp = (timestamp) => {
    if (!timestamp || timestamp === 0) return 'All data';
    const date = new Date(timestamp * 1000);
    return date.toLocaleString();
  };

  const formatDuration = (ms) => {
    if (ms < 1) return '< 1ms';
    return `${ms}ms`;
  };

  return (
    <div className="query-results">
      <h2>Query Results</h2>
      
      <div className="results-grid">
        <div className="result-card primary">
          <div className="result-label">Result</div>
          <div className="result-value">{result.result.toFixed(4)}</div>
          <div className="result-operation">{result.operation.toUpperCase()}</div>
        </div>

        <div className="result-card">
          <div className="result-label">Data Points</div>
          <div className="result-value">{result.count}</div>
        </div>

        <div className="result-card">
          <div className="result-label">Query Time</div>
          <div className="result-value">{formatDuration(result.duration_ms)}</div>
        </div>
      </div>

      <div className="results-details">
        <div className="detail-row">
          <span className="detail-label">Device ID:</span>
          <span className="detail-value">{result.device_id}</span>
        </div>
        <div className="detail-row">
          <span className="detail-label">Metric:</span>
          <span className="detail-value">{result.metric_name}</span>
        </div>
        <div className="detail-row">
          <span className="detail-label">Operation:</span>
          <span className="detail-value">{result.operation}</span>
        </div>
      </div>

      <div className="results-json">
        <h3>Raw JSON Response</h3>
        <pre>{JSON.stringify(result, null, 2)}</pre>
      </div>
    </div>
  );
};

export default QueryResults;

