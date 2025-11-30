@echo off
echo Starting MiniTrue Cluster...

REM Kill lingering processes on our standard ports
echo Cleaning up lingering ports...
for %%p in (7070 8080 8081 8082 9000 9001 9002 9100) do (
    for /f "tokens=5" %%a in ('netstat -aon ^| findstr :%%p ^| findstr LISTENING') do (
        echo Killing process on port %%p (PID: %%a)...
        taskkill /F /PID %%a 2>nul
    )
)
timeout /t 1 /nobreak > nul

REM --- Storage nodes ---
start "MiniTrue Node 1 (polaris)" cmd /k "go run cmd/minitrue-server/main.go -mode=all"
timeout /t 2 /nobreak > nul

start "MiniTrue Node 2 (sirius)" cmd /k "go run cmd/minitrue-server/main.go -mode=all"
timeout /t 2 /nobreak > nul

start "MiniTrue Node 3 (vega)" cmd /k "go run cmd/minitrue-server/main.go -mode=all"
timeout /t 2 /nobreak > nul

REM --- Stateless ingestion router ---
start "MiniTrue Router" cmd /k "go run cmd/minitrue-router/main.go --node_id=router-1 --port=7070 --tcp_port=9100 --seeds=localhost:9000,localhost:9001,localhost:9002"
timeout /t 2 /nobreak > nul

REM --- Publisher / simulator ---
start "Data Publisher" cmd /k "go run cmd/publisher/main.go --sim=true --router=http://localhost:7070/route"
timeout /t 2 /nobreak > nul

REM --- Frontend ---
start "MiniTrue Frontend" cmd /k "cd frontend && npm start"

echo All components started. Close the respective windows to terminate.
