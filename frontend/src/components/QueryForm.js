import React, { useState, useEffect } from 'react';
import './QueryForm.css';

const QueryForm = ({ onSubmit, loading, onClearResults }) => {
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

  const [formData, setFormData] = useState({
    device_id: '',
    metric_name: '',
    operation: 'avg',
    start_time: 0,
    end_time: 0,
  });

  const [deviceIds, setDeviceIds] = useState(['sensor_1', 'sensor_2', 'sensor_3']);
  const [metricNames, setMetricNames] = useState(['temperature']);
  const [startTimeDisplay, setStartTimeDisplay] = useState('');
  const [endTimeDisplay, setEndTimeDisplay] = useState('');
  const [timeError, setTimeError] = useState('');
  const [startTimeError, setStartTimeError] = useState(null);
  const [endTimeError, setEndTimeError] = useState(null);

  const parse12HourToUnix = (timeString) => {
    if (!timeString || timeString.trim() === '' || timeString === '0') {
      return { timestamp: 0, error: null };
    }
    
    try {
      const cleaned = timeString.trim().replace(/\s+/g, ' ');
      const parts = cleaned.split(' ');
      
      if (parts.length < 3) {
        return { timestamp: 0, error: 'Invalid format. Use: MM/DD/YYYY HH:MM AM/PM' };
      }
      
      const datePart = parts[0];
      const timePart = parts[1];
      const ampm = parts[2].toUpperCase();
      
      if (ampm !== 'AM' && ampm !== 'PM') {
        return { timestamp: 0, error: 'Invalid AM/PM. Use AM or PM' };
      }
      
      const dateParts = datePart.split('/');
      if (dateParts.length !== 3) {
        return { timestamp: 0, error: 'Invalid date format. Use: MM/DD/YYYY' };
      }
      
      const [month, day, year] = dateParts.map(Number);
      
      if (isNaN(month) || isNaN(day) || isNaN(year)) {
        return { timestamp: 0, error: 'Date must be numbers. Use: MM/DD/YYYY' };
      }
      
      if (month < 1 || month > 12) {
        return { timestamp: 0, error: 'Month must be between 1 and 12' };
      }
      
      if (day < 1 || day > 31) {
        return { timestamp: 0, error: 'Day must be between 1 and 31' };
      }
      
      if (year < 1900 || year > 2100) {
        return { timestamp: 0, error: 'Year must be between 1900 and 2100' };
      }
      
      const timeParts = timePart.split(':');
      if (timeParts.length < 2) {
        return { timestamp: 0, error: 'Invalid time format. Use: HH:MM' };
      }
      
      const [hours, minutes] = timeParts.map(Number);
      
      if (isNaN(hours) || isNaN(minutes)) {
        return { timestamp: 0, error: 'Time must be numbers. Use: HH:MM' };
      }
      
      if (hours < 1 || hours > 12) {
        return { timestamp: 0, error: 'Hours must be between 1 and 12' };
      }
      
      if (minutes < 0 || minutes > 59) {
        return { timestamp: 0, error: 'Minutes must be between 0 and 59' };
      }
      
      let hour24 = hours;
      if (ampm === 'PM' && hours !== 12) {
        hour24 = hours + 12;
      } else if (ampm === 'AM' && hours === 12) {
        hour24 = 0;
      }
      
      const date = new Date(year, month - 1, day, hour24, minutes || 0);
      
      if (isNaN(date.getTime())) {
        return { timestamp: 0, error: 'Invalid date. Please check the date values' };
      }
      
      const timestamp = Math.floor(date.getTime() / 1000);
      return { timestamp, error: null };
    } catch (err) {
      console.error('Error parsing time:', err);
      return { timestamp: 0, error: 'Invalid time format. Use: MM/DD/YYYY HH:MM AM/PM' };
    }
  };

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
    hours = hours ? hours : 12;
    const hoursStr = String(hours).padStart(2, '0');
    
    return `${month}/${day}/${year} ${hoursStr}:${minutes} ${ampm}`;
  };

  useEffect(() => {
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
        console.log('Using default device list');
      }
    };
    fetchDeviceIds();

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
        console.log('Using default metric list');
      }
    };
    fetchMetricNames();
  }, []);

  const validateTimeRange = (startTime, endTime, startDisplay, endDisplay, startError, endError) => {
    if (startError) {
      setTimeError(`Start time: ${startError}`);
      return false;
    }
    
    if (endError) {
      setTimeError(`End time: ${endError}`);
      return false;
    }
    
    setTimeError('');
    
    if (startTime === 0 && endTime === 0) {
      return true;
    }
    
    if (startTime === 0 && endTime !== 0 && endTime > 0) {
      return true;
    }
    
    if (endTime === 0 && startTime !== 0) {
      if (startDisplay && startDisplay.trim() !== '') {
        setTimeError('Please enter an end time or use "All Data" button.');
        return false;
      }
    }
    
    if (startTime !== 0 && endTime !== 0) {
      if (startTime >= endTime) {
        setTimeError('Start time must be before end time.');
        return false;
      }
      
      const now = Math.floor(Date.now() / 1000);
      if (startTime > now) {
        setTimeError('Start time cannot be in the future.');
        return false;
      }
      
      if (endTime > now) {
        setTimeError('End time cannot be in the future.');
        return false;
      }
    }
    
    return true;
  };

  const createTimeMask = (digits) => {
    const mask = 'MM/DD/YYYY HH:MM AM';
    const digitsArray = digits.split('');
    let result = '';
    let digitIndex = 0;
    
    for (let i = 0; i < mask.length; i++) {
      const char = mask[i];
      if (char === 'M' || char === 'D' || char === 'Y' || char === 'H' || char === 'A') {
        if (digitIndex < digitsArray.length) {
          result += digitsArray[digitIndex];
          digitIndex++;
        } else {
          result += char;
        }
      } else {
        result += char;
      }
    }
    
    return result;
  };

  const formatTimeInput = (value) => {
    const digitsOnly = value.replace(/[^0-9]/g, '');
    
    let ampm = '';
    const upperValue = value.toUpperCase();
    
    if (upperValue.includes('AM') || (upperValue.includes('A') && !upperValue.includes('PM'))) {
      ampm = 'AM';
    } else if (upperValue.includes('PM') || (upperValue.includes('P') && !upperValue.includes('AM'))) {
      ampm = 'PM';
    }
    
    if (!ampm && digitsOnly.length > 12) {
      const lastChar = digitsOnly[digitsOnly.length - 1];
      if (lastChar === '1') {
        ampm = 'AM';
        const digits = digitsOnly.slice(0, -1).slice(0, 12);
        let formatted = createTimeMask(digits);
        formatted = formatted.replace('AM', 'AM');
        return formatted;
      } else if (lastChar === '2') {
        ampm = 'PM';
        const digits = digitsOnly.slice(0, -1).slice(0, 12);
        let formatted = createTimeMask(digits);
        formatted = formatted.replace('AM', 'PM');
        return formatted;
      }
    }
    
    const digits = digitsOnly.slice(0, 12);
    
    let formatted = createTimeMask(digits);
    
    if (ampm) {
      formatted = formatted.replace('AM', ampm);
    }
    
    return formatted;
  };

  const getNewCursorPosition = (oldValue, newValue, oldCursorPos) => {
    const charsBeforeCursor = oldValue.slice(0, oldCursorPos);
    const digitPlaceholderCount = charsBeforeCursor.replace(/[\/\s:]/g, '').length;
    
    if (digitPlaceholderCount === 0) {
      for (let i = 0; i < newValue.length; i++) {
        if (/\d/.test(newValue[i]) || /[MDYHA]/.test(newValue[i])) {
          return i;
        }
      }
      return 0;
    }
    
    let charCount = 0;
    for (let i = 0; i < newValue.length; i++) {
      const char = newValue[i];
      if (/\d/.test(char) || /[MDYHA]/.test(char)) {
        charCount++;
        if (charCount === digitPlaceholderCount) {
          let nextPos = i + 1;
          while (nextPos < newValue.length && /[\/\s:]/.test(newValue[nextPos])) {
            nextPos++;
          }
          return nextPos < newValue.length ? nextPos : i + 1;
        }
      }
    }
    
    let charCount2 = 0;
    for (let i = 0; i < newValue.length; i++) {
      const char = newValue[i];
      if (/\d/.test(char) || /[MDYHA]/.test(char)) {
        charCount2++;
        if (charCount2 > digitPlaceholderCount) {
          return i;
        }
      }
    }
    
    return newValue.length;
  };

  const handleKeyDown = (e) => {
    const { name, value, selectionStart } = e.target;
    
    if (e.key === 'Backspace' && selectionStart > 0) {
      const currentPos = selectionStart;
      const charBefore = value[currentPos - 1];
      
      if (/[\/\s:]/.test(charBefore)) {
        e.preventDefault();
        let newPos = currentPos - 1;
        while (newPos > 0 && /[\/\s:]/.test(value[newPos - 1])) {
          newPos--;
        }
        if (newPos > 0) {
          const beforeSeparator = value.slice(0, newPos - 1);
          const afterSeparator = value.slice(currentPos);
          const newValue = beforeSeparator + afterSeparator;
          
          if (name === 'start_time_display') {
            const formatted = formatTimeInput(newValue);
            setStartTimeDisplay(formatted);
            setTimeout(() => {
              const digitsBefore = beforeSeparator.replace(/[^0-9MDYHA]/gi, '').length;
              let pos = 0;
              let count = 0;
              for (let i = 0; i < formatted.length; i++) {
                if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
                  if (count === digitsBefore) {
                    pos = i;
                    break;
                  }
                  count++;
                }
              }
              if (count < digitsBefore) {
                pos = formatted.length;
              }
              e.target.setSelectionRange(pos, pos);
            }, 0);
          } else if (name === 'end_time_display') {
            const formatted = formatTimeInput(newValue);
            setEndTimeDisplay(formatted);
            setTimeout(() => {
              const digitsBefore = beforeSeparator.replace(/[^0-9MDYHA]/gi, '').length;
              let pos = 0;
              let count = 0;
              for (let i = 0; i < formatted.length; i++) {
                if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
                  if (count === digitsBefore) {
                    pos = i;
                    break;
                  }
                  count++;
                }
              }
              if (count < digitsBefore) {
                pos = formatted.length;
              }
              e.target.setSelectionRange(pos, pos);
            }, 0);
          }
        }
        return;
      }
    }
    
    if (e.key === 'Delete' && selectionStart < value.length) {
      const currentPos = selectionStart;
      const charAt = value[currentPos];
      
      if (/[\/\s:]/.test(charAt)) {
        e.preventDefault();
        let newPos = currentPos + 1;
        while (newPos < value.length && /[\/\s:]/.test(value[newPos])) {
          newPos++;
        }
        if (newPos < value.length) {
          const before = value.slice(0, currentPos);
          const after = value.slice(newPos + 1);
          const newValue = before + after;
          
          if (name === 'start_time_display') {
            const formatted = formatTimeInput(newValue);
            setStartTimeDisplay(formatted);
            setTimeout(() => {
              const digitsBefore = before.replace(/[^0-9MDYHA]/gi, '').length;
              let pos = 0;
              let count = 0;
              for (let i = 0; i < formatted.length; i++) {
                if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
                  if (count === digitsBefore) {
                    pos = i;
                    break;
                  }
                  count++;
                }
              }
              e.target.setSelectionRange(pos, pos);
            }, 0);
          } else if (name === 'end_time_display') {
            const formatted = formatTimeInput(newValue);
            setEndTimeDisplay(formatted);
            setTimeout(() => {
              const digitsBefore = before.replace(/[^0-9MDYHA]/gi, '').length;
              let pos = 0;
              let count = 0;
              for (let i = 0; i < formatted.length; i++) {
                if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
                  if (count === digitsBefore) {
                    pos = i;
                    break;
                  }
                  count++;
                }
              }
              e.target.setSelectionRange(pos, pos);
            }, 0);
          }
        }
      }
    }
  };

  const handleChange = (e) => {
    const { name, value } = e.target;
    const input = e.target;
    const cursorPosition = input.selectionStart;
    const oldValue = name === 'start_time_display' ? (startTimeDisplay || 'MM/DD/YYYY HH:MM AM') : (endTimeDisplay || 'MM/DD/YYYY HH:MM AM');
    
    if (name === 'start_time_display') {
      if (value === 'MM/DD/YYYY HH:MM AM') {
        setStartTimeDisplay('');
        return;
      }
      
      const formatted = formatTimeInput(value);
      setStartTimeDisplay(formatted);
      
      const rawInputBeforeCursor = value.slice(0, cursorPosition);
      const digitCount = rawInputBeforeCursor.replace(/[^0-9MDYHA]/gi, '').length;
      
      setTimeout(() => {
        const ampmIndex = formatted.search(/\s+(AM|PM)$/i);
        const maxPos = ampmIndex > 0 ? ampmIndex : formatted.length;
        
        let pos = 0;
        let count = 0;
        for (let i = 0; i < formatted.length; i++) {
          if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
            count++;
            if (count === digitCount) {
              // Place cursor right after this character
              pos = i + 1;
              // Skip separators to get to next valid position
              while (pos < formatted.length && /[\/\s:]/.test(formatted[pos])) {
                pos++;
              }
              // Don't go beyond AM/PM section
              if (pos >= maxPos) {
                pos = maxPos;
              }
              break;
            }
          }
        }
        // If we didn't find exact match, find the position where we have digitCount digits
        if (count < digitCount) {
          // Try to find where we should be based on digit count
          count = 0;
          for (let i = 0; i < formatted.length; i++) {
            if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
              count++;
              if (count >= digitCount) {
                pos = i + 1;
                while (pos < formatted.length && /[\/\s:]/.test(formatted[pos])) {
                  pos++;
                }
                if (pos >= maxPos) {
                  pos = maxPos;
                }
                break;
              }
            }
          }
          if (count < digitCount) {
            pos = Math.min(formatted.length, maxPos);
          }
        }
        input.setSelectionRange(pos, pos);
      }, 0);
      
      const { timestamp, error } = parse12HourToUnix(formatted);
      setStartTimeError(error);
      setFormData((prev) => {
        const newData = { ...prev, start_time: timestamp };
        validateTimeRange(newData.start_time, newData.end_time, formatted, endTimeDisplay, error, endTimeError);
        return newData;
      });
    } else if (name === 'end_time_display') {
      if (value === 'MM/DD/YYYY HH:MM AM') {
        setEndTimeDisplay('');
        return;
      }
      
      const formatted = formatTimeInput(value);
      setEndTimeDisplay(formatted);
      
      const rawInputBeforeCursor = value.slice(0, cursorPosition);
      const digitCount = rawInputBeforeCursor.replace(/[^0-9MDYHA]/gi, '').length;
      
      setTimeout(() => {
        const ampmIndex = formatted.search(/\s+(AM|PM)$/i);
        const maxPos = ampmIndex > 0 ? ampmIndex : formatted.length;
        
        let pos = 0;
        let count = 0;
        for (let i = 0; i < formatted.length; i++) {
          if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
            count++;
            if (count === digitCount) {
              // Place cursor right after this character
              pos = i + 1;
              // Skip separators to get to next valid position
              while (pos < formatted.length && /[\/\s:]/.test(formatted[pos])) {
                pos++;
              }
              // Don't go beyond AM/PM section
              if (pos >= maxPos) {
                pos = maxPos;
              }
              break;
            }
          }
        }
        // If we didn't find exact match, find the position where we have digitCount digits
        if (count < digitCount) {
          // Try to find where we should be based on digit count
          count = 0;
          for (let i = 0; i < formatted.length; i++) {
            if (/\d/.test(formatted[i]) || /[MDYHA]/.test(formatted[i])) {
              count++;
              if (count >= digitCount) {
                pos = i + 1;
                while (pos < formatted.length && /[\/\s:]/.test(formatted[pos])) {
                  pos++;
                }
                if (pos >= maxPos) {
                  pos = maxPos;
                }
                break;
              }
            }
          }
          if (count < digitCount) {
            pos = Math.min(formatted.length, maxPos);
          }
        }
        input.setSelectionRange(pos, pos);
      }, 0);
      
      const { timestamp, error } = parse12HourToUnix(formatted);
      setEndTimeError(error);
      setFormData((prev) => {
        const newData = { ...prev, end_time: timestamp };
        validateTimeRange(newData.start_time, newData.end_time, startTimeDisplay, formatted, startTimeError, error);
        return newData;
      });
    } else {
      setFormData((prev) => ({
        ...prev,
        [name]: value,
      }));
    }
  };

  const handleSubmit = (e) => {
    e.preventDefault();
    
    const startParse = parse12HourToUnix(startTimeDisplay);
    const endParse = parse12HourToUnix(endTimeDisplay);
    
    setStartTimeError(startParse.error);
    setEndTimeError(endParse.error);
    
    if (!validateTimeRange(
      formData.start_time, 
      formData.end_time, 
      startTimeDisplay, 
      endTimeDisplay,
      startParse.error,
      endParse.error
    )) {
      return;
    }
    
    onSubmit(formData);
  };

  const handleDeleteAllData = async () => {
    if (!formData.device_id || !formData.metric_name) {
      alert('Please select Device ID and Metric Name before deleting data.');
      return;
    }

    const confirmMessage = `Are you sure you want to delete ALL data for:\n\nDevice: ${formData.device_id}\nMetric: ${formData.metric_name}\n\nThis action cannot be undone!`;
    if (!window.confirm(confirmMessage)) {
      return;
    }

    try {
      const response = await fetch('/delete', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({
          device_id: formData.device_id,
          metric_name: formData.metric_name,
        }),
      });

      if (!response.ok) {
        const errorData = await response.text();
        throw new Error(errorData || 'Delete failed');
      }

      const data = await response.json();
      alert(`Success! ${data.message || 'Data deleted successfully.'}`);
      
      if (onClearResults) {
        onClearResults();
      }
      
      setFormData({
        device_id: '',
        metric_name: '',
        operation: 'avg',
        start_time: 0,
        end_time: 0,
      });
      setStartTimeDisplay('');
      setEndTimeDisplay('');
      setTimeError('');
      setStartTimeError(null);
      setEndTimeError(null);
    } catch (err) {
      alert(`Error: ${err.message || 'Failed to delete data'}`);
      console.error('Delete error:', err);
    }
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
    const startFormatted = unixTo12Hour(start);
    const endFormatted = unixTo12Hour(now);
    setStartTimeDisplay(startFormatted || 'MM/DD/YYYY HH:MM AM');
    setEndTimeDisplay(endFormatted || 'MM/DD/YYYY HH:MM AM');
    setTimeError('');
    setStartTimeError(null);
    setEndTimeError(null);
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
                {formatDeviceId(deviceId)}
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
                {formatMetricName(metricName)}
              </option>
            ))}
          </select>
        </div>

        <div className="form-group">
          <label htmlFor="operation">Operation</label>
          <select
            id="operation"
            name="operation"
            value={formData.operation || 'avg'}
            onChange={handleChange}
            required
          >
            <option value="avg">Average</option>
            <option value="sum">Sum</option>
            <option value="max">Maximum</option>
            <option value="min">Minimum</option>
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
              const now = getCurrentUnixTime();
              const allDataQuery = {
                ...formData,
                start_time: 0,
                end_time: now,
              };
              setFormData(allDataQuery);
              
              const startFormatted = unixTo12Hour(0);
              const endFormatted = unixTo12Hour(now);
              setStartTimeDisplay(startFormatted || '01/01/1970 12:00 AM');
              setEndTimeDisplay(endFormatted || 'MM/DD/YYYY HH:MM AM');
              setTimeError('');
              setStartTimeError(null);
              setEndTimeError(null);
            }}
            className="time-btn"
          >
            All Data
          </button>
        </div>

        <div className="form-row">
          <div className="form-group">
            <label htmlFor="start_time_display">Start Time</label>
            <div style={{ position: 'relative', display: 'inline-block', width: '100%' }}>
              <input
                type="text"
                id="start_time_display"
                name="start_time_display"
                value={startTimeDisplay || 'MM/DD/YYYY HH:MM AM'}
                onChange={handleChange}
                onKeyDown={handleKeyDown}
                onFocus={(e) => {
                  if (e.target.value === 'MM/DD/YYYY HH:MM AM') {
                    e.target.value = '';
                    setStartTimeDisplay('');
                  }
                }}
                onBlur={(e) => {
                  if (e.target.value === '' || e.target.value.replace(/[MDYHA]/g, '').trim() === '') {
                    setStartTimeDisplay('');
                  }
                }}
                style={{ 
                  fontFamily: 'monospace', 
                  letterSpacing: '1px',
                  color: '#ffffff',
                  width: '100%',
                  paddingRight: startTimeDisplay && startTimeDisplay !== 'MM/DD/YYYY HH:MM AM' ? '60px' : '10px'
                }}
              />
              {startTimeDisplay && startTimeDisplay !== 'MM/DD/YYYY HH:MM AM' && (
                <button
                  type="button"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    const current = startTimeDisplay || '';
                    const newValue = current.includes('PM') 
                      ? current.replace('PM', 'AM') 
                      : current.replace('AM', 'PM');
                    setStartTimeDisplay(newValue);
                    const { timestamp, error } = parse12HourToUnix(newValue);
                    setStartTimeError(error);
                    setFormData((prev) => {
                      const newData = { ...prev, start_time: timestamp };
                      validateTimeRange(newData.start_time, newData.end_time, newValue, endTimeDisplay, error, endTimeError);
                      return newData;
                    });
                  }}
                  style={{
                    position: 'absolute',
                    right: '5px',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    padding: '2px 8px',
                    fontSize: '11px',
                    backgroundColor: 'transparent',
                    border: '1px solid #ffffff',
                    color: '#ffffff',
                    cursor: 'pointer',
                    borderRadius: '3px',
                    zIndex: 10
                  }}
                >
                  {startTimeDisplay.includes('PM') ? 'PM' : 'AM'}
                </button>
              )}
            </div>
            <small>Type numbers only. After 12 digits, type "A" for AM or "P" for PM (or type "1" for AM, "2" for PM).</small>
            {startTimeError && (
              <div style={{ color: '#ff4444', fontSize: '12px', marginTop: '4px' }}>
                {startTimeError}
              </div>
            )}
          </div>

          <div className="form-group">
            <label htmlFor="end_time_display">End Time</label>
            <div style={{ position: 'relative', display: 'inline-block', width: '100%' }}>
              <input
                type="text"
                id="end_time_display"
                name="end_time_display"
                value={endTimeDisplay || 'MM/DD/YYYY HH:MM AM'}
                onChange={handleChange}
                onKeyDown={handleKeyDown}
                onFocus={(e) => {
                  if (e.target.value === 'MM/DD/YYYY HH:MM AM') {
                    e.target.value = '';
                    setEndTimeDisplay('');
                  }
                }}
                onBlur={(e) => {
                  if (e.target.value === '' || e.target.value.replace(/[MDYHA]/g, '').trim() === '') {
                    setEndTimeDisplay('');
                  }
                }}
                style={{ 
                  fontFamily: 'monospace', 
                  letterSpacing: '1px',
                  color: '#ffffff',
                  width: '100%',
                  paddingRight: endTimeDisplay && endTimeDisplay !== 'MM/DD/YYYY HH:MM AM' ? '60px' : '10px'
                }}
              />
              {endTimeDisplay && endTimeDisplay !== 'MM/DD/YYYY HH:MM AM' && (
                <button
                  type="button"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    const current = endTimeDisplay || '';
                    const newValue = current.includes('PM') 
                      ? current.replace('PM', 'AM') 
                      : current.replace('AM', 'PM');
                    setEndTimeDisplay(newValue);
                    const { timestamp, error } = parse12HourToUnix(newValue);
                    setEndTimeError(error);
                    setFormData((prev) => {
                      const newData = { ...prev, end_time: timestamp };
                      validateTimeRange(newData.start_time, newData.end_time, startTimeDisplay, newValue, startTimeError, error);
                      return newData;
                    });
                  }}
                  style={{
                    position: 'absolute',
                    right: '5px',
                    top: '50%',
                    transform: 'translateY(-50%)',
                    padding: '2px 8px',
                    fontSize: '11px',
                    backgroundColor: 'transparent',
                    border: '1px solid #ffffff',
                    color: '#ffffff',
                    cursor: 'pointer',
                    borderRadius: '3px',
                    zIndex: 10
                  }}
                >
                  {endTimeDisplay.includes('PM') ? 'PM' : 'AM'}
                </button>
              )}
            </div>
            <small>Type numbers only. After 12 digits, type "A" for AM or "P" for PM (or type "1" for AM, "2" for PM).</small>
            {endTimeError && (
              <div style={{ color: '#ff4444', fontSize: '12px', marginTop: '4px' }}>
                {endTimeError}
              </div>
            )}
          </div>
        </div>

        {timeError && (
          <div className="error-message" style={{ color: '#ff4444', marginBottom: '10px', fontSize: '14px' }}>
            {timeError}
          </div>
        )}

        <div style={{ display: 'flex', gap: '10px', marginTop: '20px' }}>
          <button type="submit" disabled={loading || timeError} className="submit-btn">
            {loading ? 'Querying...' : 'Run Query'}
          </button>
          <button
            type="button"
            onClick={handleDeleteAllData}
            disabled={!formData.device_id || !formData.metric_name || loading}
            className="delete-btn"
          >
            Delete All Data
          </button>
        </div>
      </form>
    </div>
  );
};

export default QueryForm;

