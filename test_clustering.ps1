# CLUSTERING & LEADER ELECTION DEMO
Write-Host "=== RAFT CLUSTERING DEMO ===" -ForegroundColor Cyan
Write-Host "Scenario:" -ForegroundColor Gray
Write-Host "  1. Start 3 Broker Nodes (Localhost)"
Write-Host "  2. Form a Raft Cluster"
Write-Host "  3. Verify Replication (Leader -> Follower)"
Write-Host "  4. Kill Leader (Election Test)"
Write-Host "  5. Verify New Leader & Continued Operation"

# 0. Cleanup
Stop-Process -Name "server" -ErrorAction SilentlyContinue
Remove-Item -Path "data_node1" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "data_node2" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item -Path "data_node3" -Recurse -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 1

# 1. Start Node 1 (Bootstrap Leader)
Write-Host "`n[1/5] Starting Node 1 (Leader)..." -ForegroundColor Cyan
$node1 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe -config config_node1.json" -PassThru
Start-Sleep -Seconds 3

# 2. Start Node 2 & 3
Write-Host "[2/5] Starting Node 2 & 3 (Followers)..." -ForegroundColor Cyan
$node2 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe -config config_node2.json" -PassThru
$node3 = Start-Process -FilePath "powershell" -ArgumentList "-NoExit", "-Command", "./server.exe -config config_node3.json" -PassThru
Start-Sleep -Seconds 3

# 3. Join Cluster
Write-Host "`n[3/5] Joining Nodes to Cluster..." -ForegroundColor Yellow
Write-Host "  Joining Node 2..."
./nats-cli.exe -cmd join -addr localhost:4223 -sub node-2 -msg "127.0.0.1:5224"
Start-Sleep -Seconds 1

Write-Host "  Joining Node 3..."
./nats-cli.exe -cmd join -addr localhost:4223 -sub node-3 -msg "127.0.0.1:5225"
Start-Sleep -Seconds 3

Write-Host "  Cluster formed! (Check Node 1 logs for 'Added peer')" -ForegroundColor Green

# 4. Verify Replication
Write-Host "`n[4/5] Testing Replication..." -ForegroundColor Magenta
Write-Host "  Publishing to LEADER (Node 1)..."
./nats-cli.exe -cmd pub -addr localhost:4223 -sub global.events -msg "Hello Cluster!"

Write-Host "  Reading from FOLLOWER (Node 2)..."
Write-Host "  Replaying from Node 2 (should have the message)..."
./nats-cli.exe -cmd replay -addr localhost:4224 -sub global.events

# 5. Leader Election
Write-Host "`n[5/5] KILLING LEADER (Node 1)..." -ForegroundColor Red
Stop-Process -Id $node1.Id -Force
Write-Host "  Leader Dead. Waiting 5s for Election..." -ForegroundColor Gray
Start-Sleep -Seconds 5

Write-Host "  Publishing to Node 2 (Hopeful New Leader)..."
Write-Host "  (If Node 2 is not leader, it might reject or redirect - check logs)"
try {
    ./nats-cli.exe -cmd pub -addr localhost:4224 -sub global.events -msg "Message for New Leader"
} catch {
    Write-Host "  Write to Node 2 failed (maybe it is not leader yet?)" -ForegroundColor Red
}

Write-Host "  Replaying from Node 3 (Should have both messages)..."
./nats-cli.exe -cmd replay -addr localhost:4225 -sub global.events

Write-Host "`nTest Complete. Press ENTER to cleanup." -ForegroundColor Yellow
Read-Host

Stop-Process -Id $node3.Id -ErrorAction SilentlyContinue
Stop-Process -Id $node2.Id -ErrorAction SilentlyContinue
Stop-Process -Id $node1.Id -ErrorAction SilentlyContinue
