import React, { useState } from 'react';
import './QueryForm.css';

const QueryForm = ({ onSubmit, loading }) => {
  const [formData, setFormData] = useState({
    device_id: 'sensor_1',
    metric_name: 'temperature',
    operation: 'avg',
    start_time: 0,
    end_time: 0,
  });

  const handleChange = (e) => {
    const { name, value } = e.target;
    setFormData((prev) => ({
      ...prev,
      [name]: name === 'start_time' || name === 'end_time' ? parseInt(value) || 0 : value,
    }));
  };

  const handleSubmit = (e) => {
    e.preventDefault();
    onSubmit(formData);
  };

  const getCurrentUnixTime = () => {
    return Math.floor(Date.now() / 1000);
  };

  const setTimeRange = (hours) => {
    const now = getCurrentUnixTime();
    const start = now - hours * 3600;
    setFormData((prev) => ({
      ...prev,
      start_time: start,
      end_time: now,
    }));
  };

  return (
    <div className="query-form-container">
      <h2>Query Parameters</h2>
      <form onSubmit={handleSubmit} className="query-form">
        <div className="form-group">
          <label htmlFor="device_id">Device ID</label>
          <input
            type="text"
            id="device_id"
            name="device_id"
            value={formData.device_id}
            onChange={handleChange}
            placeholder="e.g., sensor_1"
            required
          />
        </div>

        <div className="form-group">
          <label htmlFor="metric_name">Metric Name</label>
          <input
            type="text"
            id="metric_name"
            name="metric_name"
            value={formData.metric_name}
            onChange={handleChange}
            placeholder="e.g., temperature"
            required
          />
        </div>

        <div className="form-group">
          <label htmlFor="operation">Operation</label>
          <select
            id="operation"
            name="operation"
            value={formData.operation}
            onChange={handleChange}
            required
          >
            <option value="avg">Average (avg)</option>
            <option value="sum">Sum</option>
            <option value="max">Maximum (max)</option>
            <option value="min">Minimum (min)</option>
          </select>
        </div>

        <div className="time-range-buttons">
          <button type="button" onClick={() => setTimeRange(1)} className="time-btn">
            Last Hour
          </button>
          <button type="button" onClick={() => setTimeRange(24)} className="time-btn">
            Last 24 Hours
          </button>
          <button type="button" onClick={() => setTimeRange(7 * 24)} className="time-btn">
            Last Week
          </button>
          <button
            type="button"
            onClick={() => setFormData((prev) => ({ ...prev, start_time: 0, end_time: 0 }))}
            className="time-btn"
          >
            All Data
          </button>
        </div>

        <div className="form-row">
          <div className="form-group">
            <label htmlFor="start_time">Start Time (Unix Timestamp)</label>
            <input
              type="number"
              id="start_time"
              name="start_time"
              value={formData.start_time}
              onChange={handleChange}
              placeholder="0 = all data"
            />
            <small>Leave as 0 to query all data</small>
          </div>

          <div className="form-group">
            <label htmlFor="end_time">End Time (Unix Timestamp)</label>
            <input
              type="number"
              id="end_time"
              name="end_time"
              value={formData.end_time}
              onChange={handleChange}
              placeholder="0 = all data"
            />
            <small>Leave as 0 to query all data</small>
          </div>
        </div>

        <button type="submit" disabled={loading} className="submit-btn">
          {loading ? '⏳ Querying...' : '🚀 Run Query'}
        </button>
      </form>
    </div>
  );
};

export default QueryForm;

