import React, { useState, useEffect } from 'react';
import './QueryForm.css';

const QueryForm = ({ onSubmit, loading }) => {
  const [formData, setFormData] = useState({
    device_id: '',
    metric_name: '',
    operation: '',
    start_time: 0,
    end_time: 0,
  });

  const [deviceIds, setDeviceIds] = useState(['sensor_1', 'sensor_2', 'sensor_3']);
  const [metricNames, setMetricNames] = useState(['temperature']);
  const [startTimeDisplay, setStartTimeDisplay] = useState('');
  const [endTimeDisplay, setEndTimeDisplay] = useState('');

  // Convert 12-hour format string to Unix timestamp
  const parse12HourToUnix = (timeString) => {
    if (!timeString || timeString.trim() === '' || timeString === '0') {
      return 0;
    }
    
    try {
      // Format: "MM/DD/YYYY HH:MM AM/PM" or "MM/DD/YYYY HH:MMAM/PM"
      const cleaned = timeString.trim().replace(/\s+/g, ' ');
      const parts = cleaned.split(' ');
      
      if (parts.length < 3) return 0;
      
      const datePart = parts[0]; // MM/DD/YYYY
      const timePart = parts[1]; // HH:MM
      const ampm = parts[2].toUpperCase(); // AM or PM
      
      const [month, day, year] = datePart.split('/').map(Number);
      const [hours, minutes] = timePart.split(':').map(Number);
      
      let hour24 = hours;
      if (ampm === 'PM' && hours !== 12) {
        hour24 = hours + 12;
      } else if (ampm === 'AM' && hours === 12) {
        hour24 = 0;
      }
      
      const date = new Date(year, month - 1, day, hour24, minutes || 0);
      return Math.floor(date.getTime() / 1000);
    } catch (err) {
      console.error('Error parsing time:', err);
      return 0;
    }
  };

  // Convert Unix timestamp to 12-hour format string
  const unixTo12Hour = (unixTimestamp) => {
    if (!unixTimestamp || unixTimestamp === 0) return '';
    
    const date = new Date(unixTimestamp * 1000);
    const month = String(date.getMonth() + 1).padStart(2, '0');
    const day = String(date.getDate()).padStart(2, '0');
    const year = date.getFullYear();
    
    let hours = date.getHours();
    const minutes = String(date.getMinutes()).padStart(2, '0');
    const ampm = hours >= 12 ? 'PM' : 'AM';
    
    hours = hours % 12;
    hours = hours ? hours : 12; // 0 should be 12
    const hoursStr = String(hours).padStart(2, '0');
    
    return `${month}/${day}/${year} ${hoursStr}:${minutes} ${ampm}`;
  };

  useEffect(() => {
    // Try to fetch device IDs from API if endpoint exists
    const fetchDeviceIds = async () => {
      try {
        const response = await fetch('/devices');
        if (response.ok) {
          const data = await response.json();
          if (Array.isArray(data)) {
            setDeviceIds(data);
          }
        }
      } catch (err) {
        // If endpoint doesn't exist, use default list
        console.log('Using default device list');
      }
    };
    fetchDeviceIds();

    // Try to fetch metric names from API if endpoint exists
    const fetchMetricNames = async () => {
      try {
        const response = await fetch('/metrics');
        if (response.ok) {
          const data = await response.json();
          if (Array.isArray(data)) {
            setMetricNames(data);
          }
        }
      } catch (err) {
        // If endpoint doesn't exist, use default list
        console.log('Using default metric list');
      }
    };
    fetchMetricNames();
  }, []);

  const handleChange = (e) => {
    const { name, value } = e.target;
    
    if (name === 'start_time_display') {
      setStartTimeDisplay(value);
      const unixTime = parse12HourToUnix(value);
      setFormData((prev) => ({ ...prev, start_time: unixTime }));
    } else if (name === 'end_time_display') {
      setEndTimeDisplay(value);
      const unixTime = parse12HourToUnix(value);
      setFormData((prev) => ({ ...prev, end_time: unixTime }));
    } else {
      setFormData((prev) => ({
        ...prev,
        [name]: value,
      }));
    }
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
    setStartTimeDisplay(unixTo12Hour(start));
    setEndTimeDisplay(unixTo12Hour(now));
  };

  return (
    <div className="query-form-container">
      <h2>Query Parameters</h2>
      <form onSubmit={handleSubmit} className="query-form">
        <div className="form-group">
          <label htmlFor="device_id">Device ID</label>
          <select
            id="device_id"
            name="device_id"
            value={formData.device_id}
            onChange={handleChange}
            required
          >
            <option value="" disabled hidden>Choose device</option>
            {deviceIds.map((deviceId) => (
              <option key={deviceId} value={deviceId}>
                {deviceId}
              </option>
            ))}
          </select>
        </div>

        <div className="form-group">
          <label htmlFor="metric_name">Metric Name</label>
          <select
            id="metric_name"
            name="metric_name"
            value={formData.metric_name}
            onChange={handleChange}
            required
          >
            <option value="" disabled hidden>Choose metric</option>
            {metricNames.map((metricName) => (
              <option key={metricName} value={metricName}>
                {metricName}
              </option>
            ))}
          </select>
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
            <option value="" disabled hidden>Choose operation</option>
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
            onClick={() => {
              setFormData((prev) => ({ ...prev, start_time: 0, end_time: 0 }));
              setStartTimeDisplay('');
              setEndTimeDisplay('');
            }}
            className="time-btn"
          >
            All Data
          </button>
        </div>

        <div className="form-row">
          <div className="form-group">
            <label htmlFor="start_time_display">Start Time</label>
            <input
              type="text"
              id="start_time_display"
              name="start_time_display"
              value={startTimeDisplay}
              onChange={handleChange}
              placeholder="Enter start time"
            />
            <small>Format: MM/DD/YYYY HH:MM AM/PM. Leave empty for all data.</small>
          </div>

          <div className="form-group">
            <label htmlFor="end_time_display">End Time</label>
            <input
              type="text"
              id="end_time_display"
              name="end_time_display"
              value={endTimeDisplay}
              onChange={handleChange}
              placeholder="Enter end time"
            />
            <small>Format: MM/DD/YYYY HH:MM AM/PM. Leave empty for all data.</small>
          </div>
        </div>

        <button type="submit" disabled={loading} className="submit-btn">
          {loading ? 'Querying...' : 'Run Query'}
        </button>
      </form>
    </div>
  );
};

export default QueryForm;

