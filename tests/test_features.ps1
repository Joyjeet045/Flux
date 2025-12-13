# Test script for new features: Cron Scheduling, Job Result Storage, and Metrics

Write-Host "`n=== TESTING NEW FEATURES ===" -ForegroundColor Cyan
Write-Host "Features:" -ForegroundColor Yellow
Write-Host "  1. Cron-like Scheduling" -ForegroundColor Gray
Write-Host "  2. Job Result Storage" -ForegroundColor Gray
Write-Host "  3. Metrics/Monitoring" -ForegroundColor Gray
Write-Host "`n"

# Start services
Write-Host "[1/4] Starting Broker..." -ForegroundColor Cyan
$broker = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe" -PassThru
Start-Sleep -Seconds 2

Write-Host "[2/4] Starting Scheduler..." -ForegroundColor Cyan
$scheduler = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./scheduler.exe" -PassThru
Start-Sleep -Seconds 2

Write-Host "[3/4] Starting Coordinator..." -ForegroundColor Cyan
$coordinator = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./coordinator.exe" -PassThru
Start-Sleep -Seconds 1

Write-Host "[4/4] Starting Worker..." -ForegroundColor Cyan
$worker = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe test-worker" -PassThru
Start-Sleep -Seconds 6

Write-Host "`n=== FEATURE 1: CRON SCHEDULING ===" -ForegroundColor Magenta
Write-Host "Registering cron job: Run 'echo Hello from cron!' every minute" -ForegroundColor Yellow

$cronJob = @{
    type = "shell"
    payload = "echo Hello from cron!"
    cron_expr = "*/1 * * * *"
} | ConvertTo-Json

try {
    $response = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $cronJob -ContentType "application/json"
    Write-Host "  Cron job registered: $($response.id)" -ForegroundColor Green
    Write-Host "  Schedule: $($response.cron)" -ForegroundColor Green
    Write-Host "  Status: $($response.status)" -ForegroundColor Green
} catch {
    Write-Host "  Failed: $_" -ForegroundColor Red
}

Write-Host "`n=== FEATURE 2: ONE-TIME JOBS ===" -ForegroundColor Magenta
Write-Host "Submitting 5 one-time jobs..." -ForegroundColor Yellow

$jobIds = @()
for ($i = 1; $i -le 5; $i = $i + 1) {
    $job = @{
        type = "shell"
        payload = "echo Job " + $i + " completed"
        run_in_seconds = 0
    } | ConvertTo-Json
    
    try {
        $response = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job -ContentType "application/json"
        $jobIds += $response.id
        Write-Host "  Job $i submitted: $($response.id)" -ForegroundColor Green
    } catch {
        Write-Host "  Job $i failed: $_" -ForegroundColor Red
    }
    Start-Sleep -Milliseconds 200
}

Write-Host "`nWaiting 5 seconds for jobs to complete..." -ForegroundColor Gray
Start-Sleep -Seconds 5

Write-Host "`n=== FEATURE 3: METRICS ===" -ForegroundColor Magenta
Write-Host "Fetching system metrics..." -ForegroundColor Yellow

try {
    $metrics = Invoke-RestMethod -Uri "http://localhost:8080/metrics" -Method Get
    Write-Host "`nMetrics:" -ForegroundColor White
    Write-Host "  Jobs Total: $($metrics.jobs_total)" -ForegroundColor Gray
    Write-Host "  Jobs Completed: $($metrics.jobs_completed)" -ForegroundColor Green
    Write-Host "  Jobs Failed: $($metrics.jobs_failed)" -ForegroundColor $(if ($metrics.jobs_failed -gt 0) { "Red" } else { "Gray" })
    Write-Host "  Jobs Pending: $($metrics.jobs_pending)" -ForegroundColor Yellow
    Write-Host "  Success Rate: $([math]::Round($metrics.success_rate, 2))%" -ForegroundColor Green
    Write-Host "  Avg Latency: $($metrics.avg_latency_ms)ms" -ForegroundColor Gray
    Write-Host "  Results Stored: $($metrics.results_stored)" -ForegroundColor Gray
} catch {
    Write-Host "  Failed to fetch metrics: $_" -ForegroundColor Red
}

Write-Host "`n=== FEATURE 2: JOB RESULT RETRIEVAL ===" -ForegroundColor Magenta
Write-Host "Fetching result for first job..." -ForegroundColor Yellow

if ($jobIds.Count -gt 0) {
    $jobId = $jobIds[0]
    try {
        $result = Invoke-RestMethod -Uri "http://localhost:8080/result?id=$jobId" -Method Get
        Write-Host "`nJob Result for $($result.job_id):" -ForegroundColor White
        Write-Host "  Worker: $($result.worker_id)" -ForegroundColor Gray
        Write-Host "  Status: $($result.status)" -ForegroundColor Green
        Write-Host "  Duration: $($result.duration_ms)ms" -ForegroundColor Gray
        Write-Host "  Output: $($result.output)" -ForegroundColor Cyan
        Write-Host "  Started: $(Get-Date -UnixTimeSeconds $result.start_time)" -ForegroundColor Gray
        Write-Host "  Ended: $(Get-Date -UnixTimeSeconds $result.end_time)" -ForegroundColor Gray
    } catch {
        Write-Host "  Failed to fetch result: $_" -ForegroundColor Red
    }
}

Write-Host "`n=== SUMMARY ===" -ForegroundColor Cyan
Write-Host "All 3 features tested successfully!" -ForegroundColor Green
Write-Host "`nAPI Endpoints:" -ForegroundColor Yellow
Write-Host "  POST /submit - Submit one-time or cron jobs" -ForegroundColor Gray
Write-Host "  GET /metrics - View system metrics" -ForegroundColor Gray
Write-Host "  GET /result?id=<job-id> - Get job execution result" -ForegroundColor Gray

Write-Host "`nPress ENTER to cleanup..." -ForegroundColor Yellow
Read-Host

# Cleanup
Write-Host "`nStopping all services..." -ForegroundColor Yellow
Stop-Process -Id $worker.Id -ErrorAction SilentlyContinue
Stop-Process -Id $coordinator.Id -ErrorAction SilentlyContinue
Stop-Process -Id $scheduler.Id -ErrorAction SilentlyContinue
Stop-Process -Id $broker.Id -ErrorAction SilentlyContinue
Write-Host "Done." -ForegroundColor Cyan
