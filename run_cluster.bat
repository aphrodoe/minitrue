@echo off
echo Starting MiniTrue Cluster...

REM Proactively kill lingering processes on our standard ports
echo Cleaning up lingering ports...
for %%p in (8080 8081 8082 9000 9001 9002) do (
    for /f "tokens=5" %%a in ('netstat -aon ^| findstr :%%p ^| findstr LISTENING') do (
        echo Killing process on port %%p (PID: %%a)...
        taskkill /F /PID %%a 2>nul
    )
)
timeout /t 1 /nobreak > nul

REM Start Node 1
start "MiniTrue Node 1" cmd /k "go run cmd/minitrue-server/main.go -mode=all"
timeout /t 2 /nobreak > nul

REM Start Node 2
start "MiniTrue Node 2" cmd /k "go run cmd/minitrue-server/main.go -mode=all"
timeout /t 2 /nobreak > nul

REM Start Node 3
start "MiniTrue Node 3" cmd /k "go run cmd/minitrue-server/main.go -mode=all"
timeout /t 2 /nobreak > nul

REM Start Data Publisher (Simulator)
start "Data Publisher" cmd /k "go run cmd/publisher/main.go --sim=true"
timeout /t 2 /nobreak > nul

REM Start Frontend
start "MiniTrue Frontend" cmd /k "cd frontend && npm start"

echo All components started! Close the respective command windows to terminate.
