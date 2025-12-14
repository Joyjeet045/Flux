# E2E TEST: Scheduler + Raft Cluster + Worker Failover
# This demonstrates the FULL stack working together

Write-Host "=== E2E: SCHEDULER + RAFT CLUSTER + WORKERS ===" -ForegroundColor Cyan
Write-Host "Components:" -ForegroundColor Yellow
Write-Host "  - 3 Raft Broker Nodes (Clustering)"
Write-Host "  - 1 Scheduler (Job Submission API)"
Write-Host "  - 1 Coordinator (Job Distribution)"
Write-Host "  - 2 Workers (Job Execution)"
Write-Host "`nScenario: Submit jobs -> Process -> Verify end-to-end"
Write-Host ""

# Cleanup
Stop-Process -Name "scheduler", "coordinator", "worker", "server" -ErrorAction SilentlyContinue
Remove-Item -Path "data_node*" -Recurse -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

# Start Raft Cluster (3 nodes)
Write-Host "[1/6] Starting Raft Cluster..." -ForegroundColor Cyan
$node1 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe -config config_node1.json" -PassThru
Start-Sleep -Seconds 3

$node2 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe -config config_node2.json" -PassThru
$node3 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe -config config_node3.json" -PassThru
Start-Sleep -Seconds 3

# Join cluster
Write-Host "  Forming cluster..."
./nats-cli.exe -cmd join -addr localhost:4223 -sub node-2 -msg "127.0.0.1:5224" | Out-Null
./nats-cli.exe -cmd join -addr localhost:4223 -sub node-3 -msg "127.0.0.1:5225" | Out-Null
Start-Sleep -Seconds 2
Write-Host "  Raft cluster ready!" -ForegroundColor Green

# Start Scheduler (connects to Node 1 - the leader)
Write-Host "`n[2/6] Starting Scheduler..." -ForegroundColor Cyan
$scheduler = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./scheduler.exe" -PassThru
Start-Sleep -Seconds 2

# Start Coordinator
Write-Host "[3/6] Starting Coordinator..." -ForegroundColor Cyan
$coordinator = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./coordinator.exe" -PassThru
Start-Sleep -Seconds 2

# Start Workers
Write-Host "[4/6] Starting Workers..." -ForegroundColor Cyan
$worker1 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-alpha" -PassThru
$worker2 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-beta" -PassThru
Write-Host "  Waiting for workers to register (heartbeats)..." -ForegroundColor Gray
Start-Sleep -Seconds 8

# Submit jobs
Write-Host "`n[5/6] Submitting Jobs..." -ForegroundColor Magenta
for ($i = 1; $i -le 10; $i++) {
    $job = @{
        type = "shell"
        payload = "echo E2E-Test-Job-$i"
        run_in_seconds = 0
    } | ConvertTo-Json
    
    try {
        Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job -ContentType "application/json" | Out-Null
        if ($i % 3 -eq 0) {
            Write-Host "  Submitted $i jobs..." -NoNewline
        }
    } catch {
        Write-Host "  Job $i failed" -ForegroundColor Red
    }
}
Write-Host "`n  All jobs submitted!" -ForegroundColor Green

# Wait for processing
Write-Host "  Waiting for job processing..." -ForegroundColor Gray
Start-Sleep -Seconds 10

# Fetch metrics
Write-Host "`n[6/6] Fetching Metrics..." -ForegroundColor Magenta
try {
    $metrics = Invoke-RestMethod -Uri "http://localhost:8080/metrics" -Method Get
    Write-Host "`nSystem Metrics:" -ForegroundColor White
    Write-Host "  Total Jobs: $($metrics.jobs_total)" -ForegroundColor Gray
    Write-Host "  Completed: $($metrics.jobs_completed)" -ForegroundColor Green
    Write-Host "  Failed: $($metrics.jobs_failed)" -ForegroundColor $(if ($metrics.jobs_failed -gt 0) { "Red" } else { "Gray" })
    Write-Host "  Success Rate: $([math]::Round($metrics.success_rate, 2))%" -ForegroundColor Green
    
    # Worker Breakdown
    Write-Host "`nWorker Performance:" -ForegroundColor White
    $workerMetrics = @()
    if ($metrics.jobs_by_worker) {
        $metrics.jobs_by_worker | Get-Member -MemberType NoteProperty | ForEach-Object {
            $workerID = $_.Name
            $count = $metrics.jobs_by_worker.$workerID
            Write-Host "  ${workerID}: $count jobs" -ForegroundColor Cyan
            
            $workerMetrics += [PSCustomObject]@{
                WorkerID = $workerID
                JobsCompleted = $count
                Timestamp = (Get-Date).ToString("yyyy-MM-dd HH:mm:ss")
            }
        }
    } else {
        Write-Host "  No per-worker metrics available" -ForegroundColor Gray
    }
    
    # Save Summary CSV
    $csvSummaryPath = "metrics_by_worker.csv"
    $workerMetrics | Export-Csv -Path $csvSummaryPath -NoTypeInformation
    
    # Process Detailed History
    $historyCsvPath = "job_history.csv"
    $historyData = @()
    if ($metrics.job_history) {
        $metrics.job_history | ForEach-Object {
            $historyData += [PSCustomObject]@{
                JobID = $_.job_id
                WorkerID = $_.worker_id
                SubmittedTime = (Get-Date -Date "1970-01-01 00:00:00Z").AddSeconds($_.submitted_at).ToLocalTime().ToString("HH:mm:ss")
                CompletionTime = (Get-Date -Date "1970-01-01 00:00:00Z").AddSeconds($_.end_time).ToLocalTime().ToString("HH:mm:ss")
                DurationMS = $_.duration_ms
            }
        }
        $historyData | Sort-Object SubmittedTime | Export-Csv -Path $historyCsvPath -NoTypeInformation
        Write-Host "Detailed job history saved to $historyCsvPath" -ForegroundColor Yellow
    }

    Write-Host "`nMetrics saved to $csvSummaryPath" -ForegroundColor Yellow
} catch {
    Write-Host "  Failed to fetch metrics: $_" -ForegroundColor Red
}

Write-Host "`n=== SUCCESS! ===" -ForegroundColor Green
Write-Host "E2E System Running:" -ForegroundColor Yellow
Write-Host "  Raft Cluster: Node-1 (Leader), Node-2, Node-3" -ForegroundColor Gray
Write-Host "  Scheduler: Accepting jobs via HTTP" -ForegroundColor Gray
Write-Host "  Coordinator: Distributing to workers" -ForegroundColor Gray
Write-Host "  Workers: Processing jobs" -ForegroundColor Gray
Write-Host "`nNOTE: Currently all components connect to Node-1 (port 4223)."
Write-Host "If Node-1 fails, manual reconnection to Node-2/3 required."
Write-Host "Full automatic failover = future enhancement."

Write-Host "`nPress ENTER to stop all services..." -ForegroundColor Yellow
Read-Host

Stop-Process -Id $worker2.Id -ErrorAction SilentlyContinue
Stop-Process -Id $worker1.Id -ErrorAction SilentlyContinue
Stop-Process -Id $coordinator.Id -ErrorAction SilentlyContinue
Stop-Process -Id $scheduler.Id -ErrorAction SilentlyContinue
Stop-Process -Id $node3.Id -ErrorAction SilentlyContinue
Stop-Process -Id $node2.Id -ErrorAction SilentlyContinue
Stop-Process -Id $node1.Id -ErrorAction SilentlyContinue
Write-Host "Done." -ForegroundColor Cyan
