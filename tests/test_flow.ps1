$ErrorActionPreference = "Stop"

Write-Host "Starting Distributed Job Scheduler Test Environment..." -ForegroundColor Cyan

# 1. Start Broker
Write-Host "1. Starting Broker (server.exe)..."
$broker = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe" -PassThru
Start-Sleep -Seconds 2

# 2. Start Scheduler
Write-Host "2. Starting Scheduler (scheduler.exe)..."
$scheduler = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./scheduler.exe" -PassThru
Start-Sleep -Seconds 1

# 3. Start Coordinator
Write-Host "3. Starting Coordinator (coordinator.exe)..."
$coordinator = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./coordinator.exe" -PassThru

# 4. Start Worker Node
Write-Host "4. Starting Worker Node (worker.exe)..."
$worker = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-1" -PassThru
Start-Sleep -Seconds 2


Write-Host "`nEnvironment Ready!" -ForegroundColor Green
Write-Host "Submitting a test job in 3 seconds..."
Start-Sleep -Seconds 3

# 5. Submit Job
$body = @{
    type = "shell"
    payload = "echo Hello from Distributed System!"
    run_in_seconds = 2
} | ConvertTo-Json

try {
    $response = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $body -ContentType "application/json"
    Write-Host "`nJob Submitted Successfully!" -ForegroundColor Green
    Write-Host "Job ID: $($response.id)"
    Write-Host "Status: $($response.status)"
} catch {
    Write-Host "Failed to submit job: $_" -ForegroundColor Red
}

Write-Host "`nEnvironment is Running." -ForegroundColor Green
Write-Host "Press ENTER to stop services and cleanup..." -ForegroundColor Yellow
Read-Host

# Cleanup
Write-Host "`nStopping all services..." -ForegroundColor Yellow
Stop-Process -Id $worker.Id -ErrorAction SilentlyContinue
Stop-Process -Id $coordinator.Id -ErrorAction SilentlyContinue
Stop-Process -Id $scheduler.Id -ErrorAction SilentlyContinue
Stop-Process -Id $broker.Id -ErrorAction SilentlyContinue
Write-Host "Done." -ForegroundColor Cyan
