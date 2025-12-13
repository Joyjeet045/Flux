$ErrorActionPreference = "Stop"

Write-Host "`n=== FLUX ADVANCED DISTRIBUTED SYSTEM TEST ===" -ForegroundColor Cyan
Write-Host "This test demonstrates:" -ForegroundColor Yellow
Write-Host "  1. Heavy concurrent load (20 jobs)" -ForegroundColor Gray
Write-Host "  2. Dynamic worker scaling (start 1, add 3 during execution)" -ForegroundColor Gray
Write-Host "  3. Worker failure and recovery" -ForegroundColor Gray
Write-Host "  4. Mixed immediate and delayed jobs" -ForegroundColor Gray
Write-Host "  5. Long-running tasks" -ForegroundColor Gray
Write-Host "  6. Coordinator redundancy (2 coordinators)" -ForegroundColor Gray
Write-Host "`n"

# Start core services
Write-Host "[1/6] Starting Broker..." -ForegroundColor Cyan
$broker = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe" -PassThru
Start-Sleep -Seconds 2

Write-Host "[2/6] Starting Scheduler..." -ForegroundColor Cyan
$scheduler = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./scheduler.exe" -PassThru
Start-Sleep -Seconds 1

Write-Host "[3/6] Starting Coordinator-1 (Primary)..." -ForegroundColor Cyan
$coordinator1 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./coordinator.exe" -PassThru
Start-Sleep -Seconds 1

Write-Host "`n=== PHASE 1: Initial Setup with 1 Worker ===" -ForegroundColor Magenta
Write-Host "Starting Worker-A..." -ForegroundColor Yellow
$workerA = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-A" -PassThru
Start-Sleep -Seconds 6

Write-Host "`n=== PHASE 2: Heavy Load Test (20 jobs) ===" -ForegroundColor Magenta
Write-Host "Submitting 20 concurrent jobs..." -ForegroundColor Yellow

$jobCount = 0
for ($i = 1; $i -le 20; $i = $i + 1) {
    $delay = 0
    if ($i % 5 -eq 0) {
        $delay = 3
    }
    
    $taskType = "quick"
    if ($i % 7 -eq 0) {
        $taskType = "slow"
    }
    
    $jobPayload = "echo Job-" + $i + "-Type-" + $taskType
    if ($taskType -eq "slow") {
        $jobPayload = "ping localhost -n 3 && echo Job-" + $i + "-completed"
    }
    
    $job = @{
        type = "shell"
        payload = $jobPayload
        run_in_seconds = $delay
    } | ConvertTo-Json
    
    try {
        $response = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job -ContentType "application/json"
        $jobCount = $jobCount + 1
        if ($i % 5 -eq 0) {
            Write-Host "  Batch $i submitted (delayed: ${delay}s)" -ForegroundColor Green
        }
    } catch {
        Write-Host "  Job $i failed: $_" -ForegroundColor Red
    }
    Start-Sleep -Milliseconds 100
}

Write-Host "  Total submitted: $jobCount jobs" -ForegroundColor Green
Write-Host "  Expected: Worker-A is overwhelmed!" -ForegroundColor Gray
Start-Sleep -Seconds 2

Write-Host "`n=== PHASE 3: Dynamic Scaling (Add 3 Workers) ===" -ForegroundColor Magenta
Write-Host "Adding Worker-B, Worker-C, Worker-D..." -ForegroundColor Yellow
$workerB = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-B" -PassThru
Start-Sleep -Milliseconds 500
$workerC = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-C" -PassThru
Start-Sleep -Milliseconds 500
$workerD = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-D" -PassThru
Write-Host "  4 workers now active!" -ForegroundColor Green
Write-Host "  Expected: New jobs distribute across all workers" -ForegroundColor Gray
Start-Sleep -Seconds 6

Write-Host "`n=== PHASE 4: Coordinator Redundancy ===" -ForegroundColor Magenta
Write-Host "Starting Coordinator-2 (Backup)..." -ForegroundColor Yellow
$coordinator2 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./coordinator.exe" -PassThru
Write-Host "  2 coordinators running (only one should assign jobs)" -ForegroundColor Gray
Start-Sleep -Seconds 2

Write-Host "`n=== PHASE 5: Submit More Jobs (Load Balancing Test) ===" -ForegroundColor Magenta
Write-Host "Submitting 10 more jobs..." -ForegroundColor Yellow

for ($i = 21; $i -le 30; $i = $i + 1) {
    $jobPayload = "echo Job-" + $i + "-on-scaled-cluster"
    $job = @{
        type = "shell"
        payload = $jobPayload
        run_in_seconds = 0
    } | ConvertTo-Json
    
    try {
        $response = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job -ContentType "application/json"
        if ($i % 3 -eq 0) {
            Write-Host "  Job $i submitted" -ForegroundColor Green
        }
    } catch {
        Write-Host "  Job $i failed: $_" -ForegroundColor Red
    }
    Start-Sleep -Milliseconds 150
}

Write-Host "  Expected: Jobs distributed across 4 workers" -ForegroundColor Gray
Start-Sleep -Seconds 3

Write-Host "`n=== PHASE 6: Worker Failure Simulation ===" -ForegroundColor Magenta
Write-Host "Simulating failure: Killing Worker-B..." -ForegroundColor Yellow
Stop-Process -Id $workerB.Id -Force -ErrorAction SilentlyContinue
Write-Host "  Worker-B terminated!" -ForegroundColor Red
Write-Host "  Expected: Remaining workers (A,C,D) continue processing" -ForegroundColor Gray
Start-Sleep -Seconds 5

Write-Host "`n=== PHASE 7: Worker Recovery ===" -ForegroundColor Magenta
Write-Host "Restarting Worker-B..." -ForegroundColor Yellow
$workerB = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-B-recovered" -PassThru
Write-Host "  Worker-B back online!" -ForegroundColor Green
Write-Host "  Expected: Worker rejoins and starts receiving jobs" -ForegroundColor Gray
Start-Sleep -Seconds 3

Write-Host "`n=== PHASE 8: Final Load Burst ===" -ForegroundColor Magenta
Write-Host "Submitting final 15 jobs..." -ForegroundColor Yellow

for ($i = 31; $i -le 45; $i = $i + 1) {
    $jobPayload = "echo Final-Job-" + $i
    $job = @{
        type = "shell"
        payload = $jobPayload
        run_in_seconds = 0
    } | ConvertTo-Json
    
    try {
        Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job -ContentType "application/json" | Out-Null
    } catch {
        Write-Host "  Job $i failed" -ForegroundColor Red
    }
    Start-Sleep -Milliseconds 50
}

Write-Host "  All jobs submitted!" -ForegroundColor Green

Write-Host "`n=== TEST SUMMARY ===" -ForegroundColor Cyan
Write-Host "Total jobs submitted: 45" -ForegroundColor Yellow
Write-Host "`nExpected Results:" -ForegroundColor Yellow
Write-Host "  Broker:" -ForegroundColor White
Write-Host "    - All messages routed successfully" -ForegroundColor Gray
Write-Host "    - NO DLQ errors" -ForegroundColor Gray
Write-Host "`n  Coordinators:" -ForegroundColor White
Write-Host "    - Both receiving heartbeats" -ForegroundColor Gray
Write-Host "    - Only coordinator-1 assigning jobs (queue group)" -ForegroundColor Gray
Write-Host "`n  Workers:" -ForegroundColor White
Write-Host "    - Worker-A: High initial load, then balanced" -ForegroundColor Gray
Write-Host "    - Worker-B: Jobs, then crash, then recovery" -ForegroundColor Gray
Write-Host "    - Worker-C: Steady job processing" -ForegroundColor Gray
Write-Host "    - Worker-D: Steady job processing" -ForegroundColor Gray
Write-Host "    - Jobs distributed based on CPU load" -ForegroundColor Gray
Write-Host "`n  Scheduler:" -ForegroundColor White
Write-Host "    - All 45 jobs accepted and scheduled" -ForegroundColor Gray
Write-Host "    - Delayed jobs executed after timer" -ForegroundColor Gray

Write-Host "`n`n=== Observation Period ===" -ForegroundColor Green
Write-Host "System running with 4 workers + 2 coordinators" -ForegroundColor Yellow
Write-Host "Watch the logs for load distribution patterns..." -ForegroundColor Gray
Write-Host "`nPress ENTER when ready to cleanup..." -ForegroundColor Yellow
Read-Host

# Cleanup
Write-Host "`nStopping all services..." -ForegroundColor Yellow
Stop-Process -Id $workerD.Id -ErrorAction SilentlyContinue
Stop-Process -Id $workerC.Id -ErrorAction SilentlyContinue
Stop-Process -Id $workerB.Id -ErrorAction SilentlyContinue
Stop-Process -Id $workerA.Id -ErrorAction SilentlyContinue
Stop-Process -Id $coordinator2.Id -ErrorAction SilentlyContinue
Stop-Process -Id $coordinator1.Id -ErrorAction SilentlyContinue
Stop-Process -Id $scheduler.Id -ErrorAction SilentlyContinue
Stop-Process -Id $broker.Id -ErrorAction SilentlyContinue
Write-Host "Done." -ForegroundColor Cyan
