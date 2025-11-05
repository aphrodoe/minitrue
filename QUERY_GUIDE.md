# Detailed Query Guide - Step 4

## Method 1: Using Web Dashboard (Easiest)

### Step-by-Step:

1. **Open the HTML file:**
   - Navigate to: `F:\minitrue\web\index.html`
   - Double-click it or right-click → "Open with" → Your browser

2. **Fill in the form fields:**
   - **Device ID**: `sensor_1` (or `sensor_2`, `sensor_3`)
   - **Metric**: `temperature`
   - **Operation**: Select from dropdown (avg, sum, max, min)
   - **Start Time**: `0` (means "all data")
   - **End Time**: `0` (means "all data")

3. **Click "Run Query" button**

4. **See the result** displayed in the Result box below

### Example Result:
```json
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "operation": "avg",
  "result": 23.45,
  "count": 10,
  "duration_ms": 5
}
```

## Method 2: Using PowerShell (Command Line)

### Step 1: Open PowerShell
- Press `Windows + X` → Select "Windows PowerShell" or "Terminal"
- Or search for "PowerShell" in Start menu

### Step 2: Navigate to project directory (if not already there)
```powershell
cd F:\minitrue
```

### Step 3: Create a query JSON file (optional, for easier editing)

Create a file called `query.json`:
```powershell
@"
{
  "device_id": "sensor_1",
  "metric_name": "temperature",
  "operation": "avg",
  "start_time": 0,
  "end_time": 0
}
"@ | Out-File -FilePath query.json -Encoding utf8
```

### Step 4: Send the query using PowerShell

**Option A: Using Invoke-RestMethod (Recommended)**
```powershell
$body = @{
    device_id = "sensor_1"
    metric_name = "temperature"
    operation = "avg"
    start_time = 0
    end_time = 0
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
```

**Option B: Using the JSON file**
```powershell
$body = Get-Content query.json -Raw
Invoke-RestMethod -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
```

**Option B: Using Invoke-WebRequest (shows more details)**
```powershell
$body = @{
    device_id = "sensor_1"
    metric_name = "temperature"
    operation = "avg"
    start_time = 0
    end_time = 0
} | ConvertTo-Json

$response = Invoke-WebRequest -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
$response.Content | ConvertFrom-Json
```

### Example Output:
```
device_id   : sensor_1
metric_name : temperature
operation   : avg
result      : 23.45
count       : 10
duration_ms : 5
```

## Query Examples

### Example 1: Get Average Temperature
```powershell
$body = @{
    device_id = "sensor_1"
    metric_name = "temperature"
    operation = "avg"
    start_time = 0
    end_time = 0
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
```

### Example 2: Get Maximum Temperature
```powershell
$body = @{
    device_id = "sensor_1"
    metric_name = "temperature"
    operation = "max"
    start_time = 0
    end_time = 0
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
```

### Example 3: Get Sum of All Values
```powershell
$body = @{
    device_id = "sensor_1"
    metric_name = "temperature"
    operation = "sum"
    start_time = 0
    end_time = 0
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
```

### Example 4: Query Specific Time Range
```powershell
# Get current Unix timestamp (seconds since 1970)
$now = [int][double]::Parse((Get-Date -UFormat %s))
$oneHourAgo = $now - 3600

$body = @{
    device_id = "sensor_1"
    metric_name = "temperature"
    operation = "avg"
    start_time = $oneHourAgo
    end_time = $now
} | ConvertTo-Json

Invoke-RestMethod -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
```

## Understanding the Response

The query returns a JSON object with these fields:

- **device_id**: The sensor device you queried
- **metric_name**: The metric type (e.g., "temperature")
- **operation**: The calculation performed (avg, sum, max, min)
- **result**: The calculated result (number)
- **count**: How many data points were used in the calculation
- **duration_ms**: How long the query took (in milliseconds)

## Troubleshooting

### Error: "Cannot connect to the remote server"
- **Solution**: Make sure the server is running on port 8080
- Check: `go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080`

### Error: "count: 0" or "result: 0"
- **Solution**: Wait for the publisher to send some data first
- Check the publisher terminal to see if it's publishing messages

### Error: "missing fields"
- **Solution**: Make sure all required fields are provided:
  - `device_id` (required)
  - `metric_name` (required)
  - `operation` (required: avg, sum, max, or min)
  - `start_time` (optional, use 0 for all)
  - `end_time` (optional, use 0 for all)

### Error: CORS or browser errors
- **Solution**: The web dashboard might have CORS issues. Use PowerShell instead, or check browser console (F12) for errors.

## Quick Test Script

Save this as `test_query.ps1`:

```powershell
# Test Query Script
Write-Host "Testing Minitrue Query API..." -ForegroundColor Green

$body = @{
    device_id = "sensor_1"
    metric_name = "temperature"
    operation = "avg"
    start_time = 0
    end_time = 0
} | ConvertTo-Json

Write-Host "Sending query..." -ForegroundColor Yellow
Write-Host "Request: $body" -ForegroundColor Cyan

try {
    $response = Invoke-RestMethod -Uri "http://localhost:8080/query" -Method POST -Body $body -ContentType "application/json"
    Write-Host "`n=== Query Result ===" -ForegroundColor Green
    $response | Format-List
} catch {
    Write-Host "Error: $_" -ForegroundColor Red
    Write-Host "Make sure the server is running on port 8080" -ForegroundColor Yellow
}
```

Run it:
```powershell
.\test_query.ps1
```

