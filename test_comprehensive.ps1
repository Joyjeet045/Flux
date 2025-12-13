$ErrorActionPreference = "Stop"

Write-Host "`n=== FLUX DISTRIBUTED JOB SCHEDULER - COMPREHENSIVE TEST ===" -ForegroundColor Cyan
Write-Host "This test demonstrates:" -ForegroundColor Yellow
Write-Host "  1. Immediate job execution" -ForegroundColor Gray
Write-Host "  2. Delayed job execution (5s)" -ForegroundColor Gray
Write-Host "  3. Multiple concurrent jobs" -ForegroundColor Gray
Write-Host "  4. Different shell commands" -ForegroundColor Gray
Write-Host "  5. Worker load balancing (2 workers)" -ForegroundColor Gray
Write-Host "`n"

# Start services
Write-Host "[1/6] Starting Broker..." -ForegroundColor Cyan
$broker = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe" -PassThru
Start-Sleep -Seconds 2

Write-Host "[2/6] Starting Scheduler..." -ForegroundColor Cyan
$scheduler = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./scheduler.exe" -PassThru
Start-Sleep -Seconds 1

Write-Host "[3/6] Starting Coordinator..." -ForegroundColor Cyan
$coordinator = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./coordinator.exe" -PassThru
Start-Sleep -Seconds 1

Write-Host "[4/6] Starting Worker-1..." -ForegroundColor Cyan
$worker1 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-1" -PassThru

Write-Host "[5/6] Starting Worker-2..." -ForegroundColor Cyan
$worker2 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./worker.exe worker-2" -PassThru
Start-Sleep -Seconds 2

Write-Host "[6/6] All services started!`n" -ForegroundColor Green

# Test 1: Immediate execution
Write-Host "`n=== TEST 1: Immediate Job Execution ===" -ForegroundColor Magenta
Write-Host "Submitting job to run immediately..." -ForegroundColor Yellow

$job1 = @{
    type = "shell"
    payload = "echo Test-1: Immediate execution successful"
    run_in_seconds = 0
} | ConvertTo-Json

try {
    $response1 = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job1 -ContentType "application/json"
    Write-Host "Success Job submitted: $($response1.id)" -ForegroundColor Green
    Write-Host "  Expected in Worker window: Received Job ... Test-1" -ForegroundColor Gray
    Start-Sleep -Seconds 2
} catch {
    Write-Host "Failed: $_" -ForegroundColor Red
}

# Test 2: Delayed execution
Write-Host "`n=== TEST 2: Delayed Job Execution - 5 seconds ===" -ForegroundColor Magenta
Write-Host "Submitting job to run in 5 seconds..." -ForegroundColor Yellow

$job2 = @{
    type = "shell"
    payload = "echo Test-2: Delayed execution successful"
    run_in_seconds = 5
} | ConvertTo-Json

try {
    $response2 = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job2 -ContentType "application/json"
    Write-Host "Success Job submitted: $($response2.id)" -ForegroundColor Green
    Write-Host "  Waiting 5 seconds for execution..." -ForegroundColor Gray
    Start-Sleep -Seconds 6
} catch {
    Write-Host "Failed: $_" -ForegroundColor Red
}

# Test 3: Multiple concurrent jobs
Write-Host "`n=== TEST 3: Load Balancing Across 2 Workers ===" -ForegroundColor Magenta
Write-Host "Submitting 4 jobs..." -ForegroundColor Yellow

for ($i = 1; $i -le 4; $i = $i + 1) {
    $jobPayload = "echo Test-3-Job-" + $i + ": Processing"
    $job = @{
        type = "shell"
        payload = $jobPayload
        run_in_seconds = 0
    } | ConvertTo-Json
    
    try {
        $response = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job -ContentType "application/json"
        Write-Host "Success Job $i submitted: $($response.id)" -ForegroundColor Green
    } catch {
        Write-Host "Failed Job $i : $_" -ForegroundColor Red
    }
    Start-Sleep -Milliseconds 200
}

Write-Host "`nExpected: Jobs distributed across worker-1 and worker-2" -ForegroundColor Gray
Start-Sleep -Seconds 3

# Test 4: Different commands
Write-Host "`n=== TEST 4: Different Shell Commands ===" -ForegroundColor Magenta

Write-Host "Test 4a: Directory listing..." -ForegroundColor Yellow
$job4a = @{
    type = "shell"
    payload = "dir *.exe"
    run_in_seconds = 0
} | ConvertTo-Json

try {
    $response4a = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job4a -ContentType "application/json"
    Write-Host "Success Directory job: $($response4a.id)" -ForegroundColor Green
    Start-Sleep -Seconds 1
} catch {
    Write-Host "Failed: $_" -ForegroundColor Red
}

Write-Host "Test 4b: Environment variable..." -ForegroundColor Yellow
$job4b = @{
    type = "shell"
    payload = "echo User: %USERNAME%"
    run_in_seconds = 0
} | ConvertTo-Json

try {
    $response4b = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job4b -ContentType "application/json"
    Write-Host "Success Environment job: $($response4b.id)" -ForegroundColor Green
    Start-Sleep -Seconds 1
} catch {
    Write-Host "Failed: $_" -ForegroundColor Red
}

Write-Host "Test 4c: System info..." -ForegroundColor Yellow
$job4c = @{
    type = "shell"
    payload = "hostname"
    run_in_seconds = 0
} | ConvertTo-Json

try {
    $response4c = Invoke-RestMethod -Uri "http://localhost:8080/submit" -Method Post -Body $job4c -ContentType "application/json"
    Write-Host "Success Hostname job: $($response4c.id)" -ForegroundColor Green
    Start-Sleep -Seconds 1
} catch {
    Write-Host "Failed: $_" -ForegroundColor Red
}

# Summary
Write-Host "`n=== TEST SUMMARY ===" -ForegroundColor Cyan
Write-Host "`nExpected Results:" -ForegroundColor Yellow
Write-Host "  Broker: Message routing logs NO DLQ errors" -ForegroundColor Gray
Write-Host "  Scheduler: Job submitted and Scheduling Job logs" -ForegroundColor Gray
Write-Host "  Coordinator: DEBUG Received MSG and Assigning Job logs" -ForegroundColor Gray
Write-Host "  Workers: Received Job and Job completed logs" -ForegroundColor Gray

Write-Host "`n=== Test Complete ===" -ForegroundColor Green
Write-Host "Press ENTER to stop all services..." -ForegroundColor Yellow
Read-Host

# Cleanup
Write-Host "`nStopping services..." -ForegroundColor Yellow
Stop-Process -Id $worker2.Id -ErrorAction SilentlyContinue
Stop-Process -Id $worker1.Id -ErrorAction SilentlyContinue
Stop-Process -Id $coordinator.Id -ErrorAction SilentlyContinue
Stop-Process -Id $scheduler.Id -ErrorAction SilentlyContinue
Stop-Process -Id $broker.Id -ErrorAction SilentlyContinue
Write-Host "Done." -ForegroundColor Cyan
