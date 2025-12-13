# Test Recovery Scenario: Worker Crash -> DLQ -> Coordinator Recovery -> New Worker

Write-Host "=== TEST: WORKER CRASH & RECOVERY ===" -ForegroundColor Cyan

# 0. Cleanup
Stop-Process -Name "scheduler", "coordinator", "worker", "server" -ErrorAction SilentlyContinue
Remove-Item -Path "data_recovery_test" -Recurse -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

# 1. Start Broker with FAST FAIL config
Write-Host "[1/5] Starting Broker (5s timeout)..." -ForegroundColor Yellow
$broker = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe -config config_fast_fail.json" -PassThru
Start-Sleep -Seconds 2

# 2. Start Coordinator
Write-Host "[2/5] Starting Coordinator..." -ForegroundColor Yellow
$coordinator = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./coordinator.exe" -PassThru
Start-Sleep -Seconds 1

# 3. Start "Reliable" Worker
Write-Host "[3/5] Starting Worker-Slow (Reliable)..." -ForegroundColor Green
$workerSlow = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-slow" -PassThru
Start-Sleep -Seconds 2

# 4. Start "Crashy" Worker
Write-Host "[4/5] Starting Worker-Fast (Will Crash)..." -ForegroundColor Red
$workerFast = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-fast" -PassThru
Start-Sleep -Seconds 5

# 5. Submit SUICIDE JOB
Write-Host "[5/5] Submitting SUICIDE JOB..." -ForegroundColor Magenta
# We manually craft the JSON for "CRASH_IMMEDIATELY"
$job = @{
    type = "shell"
    payload = "CRASH_IMMEDIATELY"
    run_in_seconds = 0
} | ConvertTo-Json

# Submit via Scheduler API (assuming scheduler is running, oh wait, I need to start scheduler too)
# Let's start scheduler briefly just to submit
$scheduler = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./scheduler.exe" -PassThru
Start-Sleep -Seconds 2

try {
    Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job -ContentType "application/json"
    Write-Host "Job Submitted to trigger crash!" -ForegroundColor Green
} catch {
    Write-Host "Failed to submit job: $_" -ForegroundColor Red
}

Write-Host "`nMonitoring for 20 seconds..." -ForegroundColor Cyan
Write-Host "EXPECTED SEQUENCE:" -ForegroundColor Gray
Write-Host "1. Worker-Fast receives job and DIES"
Write-Host "2. Broker waits 5s... Retries... Waits 5s... DLQ"
Write-Host "3. Coordinator sees DLQ message"
Write-Host "4. Coordinator re-assigns to Worker-Slow"
Write-Host "5. Worker-Slow executes job (or fails if payload is still crash, wait, payload is unchanged)"
Write-Host "   ACTUALLY: Worker-Slow will receive 'CRASH_IMMEDIATELY' and ALSO CRASH."
Write-Host "   This proves delivery, but kills my second worker too. That's fine for the test!"
Write-Host "   Ideally we'd want a job that crashes only specific worker ID, but payload is shared."
Write-Host "   If Worker-Slow crashes too, it PROVES the job was recovered and re-delivered!"

Start-Sleep -Seconds 20

Write-Host "Done. Check Coordinator output for 'RECOVERING FAILED JOB'." -ForegroundColor Cyan
Read-Host "Press Enter to cleanup"

Stop-Process -Id $workerFast.Id -ErrorAction SilentlyContinue
Stop-Process -Id $workerSlow.Id -ErrorAction SilentlyContinue
Stop-Process -Id $coordinator.Id -ErrorAction SilentlyContinue
Stop-Process -Id $scheduler.Id -ErrorAction SilentlyContinue
Stop-Process -Id $broker.Id -ErrorAction SilentlyContinue
