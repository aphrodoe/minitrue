import React from 'react';
import './QueryResults.css';

const QueryResults = ({ result }) => {
  if (!result) return null;

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
          <div className="result-operation">{result.operation.toUpperCase()}</div>
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
    </div>
  );
};

export default QueryResults;

