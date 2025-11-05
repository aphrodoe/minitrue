# 🚀 Complete Step-by-Step Guide to Run Minitrue

## Prerequisites Checklist

Before starting, ensure you have:
- ✅ Go 1.21+ installed (`go version`)
- ✅ Node.js 16+ installed (`node --version`)
- ✅ npm installed (`npm --version`)
- ✅ MQTT broker (Mosquitto) installed or Docker

---

## Step 1: Install Go Dependencies

Open **Terminal 1** (PowerShell or Command Prompt):

```powershell
# Navigate to project directory (if not already there)
cd F:\minitrue

# Install Go dependencies
go mod download
```

**Expected output:** Dependencies download silently.

---

## Step 2: Start MQTT Broker

You need Mosquitto running. Choose one option:

### Option A: Using Docker (Easiest)
```powershell
docker run -it -p 1883:1883 eclipse-mosquitto
```

### Option B: Using Installed Mosquitto
```powershell
# Windows
mosquitto

# Linux/macOS
mosquitto -d
```

**Check:** Broker should be running on port 1883.

---

## Step 3: Start the Backend Server

Keep **Terminal 1** open, or open a new **Terminal 2**:

```powershell
cd F:\minitrue
go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080
```

**Expected output:**
```
Starting node ing1 in mode=all (broker=tcp://localhost:1883)
[ing1] Ingestion service started
[ing1] Query HTTP server running on :8080
Query HTTP listening on :8080
```

**✅ Success indicator:** You see "Query HTTP listening on :8080"

**Keep this terminal running!** ⚠️ Don't close it.

---

## Step 4: Start the Data Publisher (Simulator)

Open **Terminal 3** (new terminal):

```powershell
cd F:\minitrue
go run -tags=no_serial ./cmd/publisher/main_no_serial.go --sim=true
```

**Expected output:**
```
published simulated sensor_1 -> {"device_id":"sensor_1","metric_name":"temperature","timestamp":...,"value":23.45}
published simulated sensor_2 -> {"device_id":"sensor_2",...}
```

**✅ Success indicator:** You see "published simulated" messages every second.

**Keep this terminal running!** ⚠️ Don't close it.

---

## Step 5: Install React Frontend Dependencies

Open **Terminal 4** (new terminal):

```powershell
cd F:\minitrue\web
npm install
```

**Expected output:**
```
added 1234 packages, and audited 1235 packages in 2m
```

**This may take 2-5 minutes** on first run.

---

## Step 6: Start the React Frontend

In the same **Terminal 4** (still in `web` directory):

```powershell
npm start
```

**Expected output:**
```
Compiled successfully!

You can now view minitrue-frontend in the browser.

  Local:            http://localhost:3000
  On Your Network:  http://192.168.x.x:3000

Note that the development build is not optimized.
```

**✅ Success indicator:** Browser automatically opens to `http://localhost:3000`

**Keep this terminal running!** ⚠️ Don't close it.

---

## Step 7: Query Data in the Frontend

In your browser at `http://localhost:3000`:

1. **Fill in the form:**
   - Device ID: `sensor_1` (or `sensor_2`, `sensor_3`)
   - Metric Name: `temperature`
   - Operation: Select `avg` (or `sum`, `max`, `min`)
   - Time Range: Click "All Data" button (or use custom timestamps)

2. **Click "🚀 Run Query"**

3. **View results:**
   - See the calculated result
   - Check data point count
   - View query execution time
   - See raw JSON response

---

## 🎯 Quick Reference: All Commands

### Terminal 1: Go Dependencies
```powershell
cd F:\minitrue
go mod download
```

### Terminal 2: Backend Server
```powershell
cd F:\minitrue
go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080
```

### Terminal 3: Data Publisher
```powershell
cd F:\minitrue
go run -tags=no_serial ./cmd/publisher/main_no_serial.go --sim=true
```

### Terminal 4: React Frontend
```powershell
cd F:\minitrue\web
npm install        # Only needed first time
npm start
```

---

## 📊 Verification Checklist

After all steps, verify:

- [ ] **Terminal 2**: Shows "Query HTTP listening on :8080"
- [ ] **Terminal 3**: Shows "published simulated" messages continuously
- [ ] **Terminal 4**: Shows "Compiled successfully" and browser opens
- [ ] **Browser**: Frontend loads at `http://localhost:3000`
- [ ] **Query works**: You can run queries and see results

---

## 🔧 Troubleshooting

### Problem: "Cannot connect to MQTT broker"
**Solution:** 
- Check if Mosquitto is running
- Try: `docker ps` (if using Docker)
- Restart Mosquitto

### Problem: "Port 8080 already in use"
**Solution:**
```powershell
# Change port in backend command:
go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8081

# Then update React proxy in web/package.json:
"proxy": "http://localhost:8081"
```

### Problem: "npm install fails"
**Solution:**
- Check Node.js version: `node --version` (needs 16+)
- Clear npm cache: `npm cache clean --force`
- Try: `npm install --legacy-peer-deps`

### Problem: "Frontend shows errors"
**Solution:**
- Check browser console (F12 → Console)
- Ensure backend is running on port 8080
- Check CORS headers in backend

### Problem: "Query returns count: 0"
**Solution:**
- Wait for publisher to send some data (wait 10-20 seconds)
- Check publisher terminal shows "published simulated" messages
- Verify device_id matches what publisher sends (sensor_1, sensor_2, sensor_3)

---

## 🎬 Complete Startup Script (Windows)

Save this as `start_all.bat` in project root:

```batch
@echo off
echo Starting Minitrue Complete System...

echo.
echo [1/4] Starting MQTT Broker...
start "MQTT Broker" cmd /k "docker run -it -p 1883:1883 eclipse-mosquitto || mosquitto"

timeout /t 3 /nobreak >nul

echo.
echo [2/4] Starting Backend Server...
start "Backend Server" cmd /k "cd /d %~dp0 && go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080"

timeout /t 3 /nobreak >nul

echo.
echo [3/4] Starting Data Publisher...
start "Data Publisher" cmd /k "cd /d %~dp0 && go run -tags=no_serial ./cmd/publisher/main_no_serial.go --sim=true"

timeout /t 3 /nobreak >nul

echo.
echo [4/4] Starting React Frontend...
start "React Frontend" cmd /k "cd /d %~dp0\web && npm start"

echo.
echo ========================================
echo All services starting!
echo ========================================
echo Backend: http://localhost:8080
echo Frontend: http://localhost:3000
echo ========================================
echo.
echo Press any key to exit (services will keep running)
pause >nul
```

Run it: `.\start_all.bat`

---

## 🛑 Stopping Everything

To stop all services:

1. **Close Terminal 2** (Backend) - Press `Ctrl+C`
2. **Close Terminal 3** (Publisher) - Press `Ctrl+C`
3. **Close Terminal 4** (React) - Press `Ctrl+C`
4. **Stop MQTT Broker** - Press `Ctrl+C` in Docker terminal or stop Mosquitto service

---

## ✅ Success Indicators

You know everything is working when:

1. ✅ Backend terminal shows: `Query HTTP listening on :8080`
2. ✅ Publisher terminal shows continuous: `published simulated sensor_X`
3. ✅ Browser opens automatically to `http://localhost:3000`
4. ✅ Frontend form loads successfully
5. ✅ Running a query shows results with `count > 0`

---

## 📝 Next Steps

Once everything is running:

1. **Try different queries:**
   - Change Device ID (sensor_1, sensor_2, sensor_3)
   - Try different operations (avg, sum, max, min)
   - Use time range buttons

2. **Monitor data:**
   - Watch publisher terminal for incoming data
   - Check backend terminal for query logs

3. **Explore features:**
   - Run multiple queries
   - Compare results
   - Check query performance (duration_ms)

---

**Need help?** Check the main README.md for more details!

