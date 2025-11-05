@echo off
echo Starting Minitrue Cluster...

REM Create logs directory
if not exist logs mkdir logs

REM Start Node 1 (Ingestion + Query)
echo Starting node ing1 (ingestion + query) on port 8080...
start /B go run ./cmd/minitrue-server/main.go --mode=all --node_id=ing1 --port=8080 > logs/ing1.log 2>&1
timeout /t 2 /nobreak >nul

REM Start Node 2 (Ingestion only)
echo Starting node ing2 (ingestion) on port 8081...
start /B go run ./cmd/minitrue-server/main.go --mode=ingestion --node_id=ing2 > logs/ing2.log 2>&1
timeout /t 2 /nobreak >nul

REM Start Node 3 (Ingestion only)
echo Starting node ing3 (ingestion) on port 8082...
start /B go run ./cmd/minitrue-server/main.go --mode=ingestion --node_id=ing3 > logs/ing3.log 2>&1
timeout /t 2 /nobreak >nul

REM Start publisher
echo Starting simulated publisher...
start /B go run -tags=no_serial ./cmd/publisher/main_no_serial.go --sim=true > logs/publisher.log 2>&1

echo.
echo Cluster started!
echo   Node 1 (ing1): http://localhost:8080/query
echo   Logs: logs\ing1.log
echo.
echo Open web\index.html in your browser to query data
echo.
echo Press Ctrl+C to stop all nodes
pause



