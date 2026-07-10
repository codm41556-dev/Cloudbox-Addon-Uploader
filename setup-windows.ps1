<#
Cloudbox local test setup (Windows)

HOW TO RUN THIS (important):
  Don't double-click this file, and don't right-click > "Run with
  PowerShell" - both can make the window disappear before you can read
  anything. Instead:
    1. Press Start, type "PowerShell", open it.
    2. cd into the folder containing this script, e.g.:
         cd "C:\cloudbox-test"
    3. Unblock-File .\setup-windows.ps1
    4. .\setup-windows.ps1 -DbPassword "pick-a-password"
  Because the PowerShell window is already open before you run it, it
  stays open no matter what happens. This version also pauses at the
  very end (success OR error) so it won't vanish either way.

WHAT THIS AUTOMATES:
  - detects your LAN IP and adds a hosts file entry for it (needs admin -
    see below), working around GMod's DHTML browser blocking raw IPs
  - finds or downloads MinIO (local stand-in for AWS S3) and starts it
  - creates the two buckets the backend expects (or tells you how to,
    by hand, if that part can't be automated on your machine)
  - applies schema.sql and creates the `cloudbox` DB user
  - builds cloudbox-backend.exe and forecaster.exe
  - launches both in their own windows
  - finds or downloads cloudflared and opens a free, anonymous Cloudflare
    quick tunnel to the backend - GMod's server-side http.Fetch (unlike
    the DHTML panel) hard-blocks every private network address at the
    engine level, confirmed by testing, so cloudbox_api_url needs a real
    public URL - a hostname resolving to a private IP isn't enough for it

WHAT YOU INSTALL YOURSELF FIRST:
  - Go:      https://go.dev/dl/
  - MariaDB: https://mariadb.org/download/  (or mariadb.com/downloads if
             that page gives you trouble - see the chat reply)

USAGE
  Put this script in the same folder as:
    cloudbox-backend\      (extracted from cloudbox-backend.zip)
    cloudbox13\            (extracted from cloudbox13.zip - not built, it's Lua)
    forecaster\            (extracted from forecaster.zip)
    schema.sql
    launch-backend.ps1     (companion file - keep next to this script)
    launch-forecaster.ps1  (companion file - keep next to this script)
  If you already downloaded a MinIO .exe yourself, drop it in this same
  folder too (any filename ending in .exe - the script will find it).

  .\setup-windows.ps1 -DbPassword "choose-a-password" -SteamApiKey "..."

  SteamApiKey (from https://steamcommunity.com/dev/apikey) can be left
  out for now - the backend still starts, you just can't finish "sign in
  through Steam" on the upload page until you add one and restart it.

  IMPORTANT: GMod's in-game DHTML browser blocks URLs that are literally a
  raw IP address (127.0.0.1, your LAN IP, whatever) - looks like a broad
  anti-SSRF measure. The workaround: a hostname that isn't shaped like a
  bare IP, pointed at your real LAN IP via the Windows hosts file. This
  script does that automatically (needs admin rights - see below), using
  the hostname "cloudboxlocal.com" pointed at your LAN IP.
  Override either if needed:
    .\setup-windows.ps1 -DbPassword "..." -LanIP "10.0.0.31" -Hostname "cloudboxlocal.com"

  Editing the hosts file needs Administrator rights. If you don't run this
  script as admin, it'll skip that step and print the one line to add
  yourself (Notepad as Administrator > open
  C:\Windows\System32\drivers\etc\hosts > add the line > save).
#>

param(
    [string]$DbPassword = "cloudboxlocaltest",
    [string]$SteamApiKey = "",
    [string]$MySqlRootPassword = "",
    [string]$LanIP = "",
    [string]$Hostname = "cloudboxlocal.com"
)

function Write-Step($msg) {
    Write-Host ""
    Write-Host "==> $msg" -ForegroundColor Cyan
}

function Pause-Exit($code) {
    Write-Host ""
    Read-Host "Press Enter to close this window"
    exit $code
}

$root = $PSScriptRoot
Set-Location $root

try {
    # ---------------------------------------------------------------------
    Write-Step "Checking Go is installed"
    $goCmd = Get-Command go.exe -ErrorAction SilentlyContinue
    if (-not $goCmd) {
        throw "Go isn't on PATH. Install it from https://go.dev/dl/ (the .msi installer), then open a BRAND NEW PowerShell window (so it picks up the updated PATH) and run this script again."
    }
    go version

    foreach ($launcher in @("launch-backend.ps1", "launch-forecaster.ps1")) {
        if (-not (Test-Path "$root\$launcher")) {
            throw "Missing $launcher - it needs to be in this same folder alongside setup-windows.ps1 (re-download it if you don't have it)."
        }
    }

    # ---------------------------------------------------------------------
    Write-Step "Figuring out your LAN IP"
    # GMod's in-game DHTML browser blocks 127.0.0.1/loopback requests as an
    # anti-SSRF measure, so the servers need to be reachable at a real LAN
    # address for the in-game side of things to work at all.
    if ($LanIP -eq "") {
        $candidates = Get-NetIPAddress -AddressFamily IPv4 -ErrorAction SilentlyContinue |
            Where-Object {
                $_.IPAddress -notlike "127.*" -and
                $_.IPAddress -notlike "169.254.*" -and
                $_.InterfaceAlias -notmatch "Loopback|vEthernet|WSL|Virtual|Hyper-V"
            } |
            Select-Object -ExpandProperty IPAddress

        if ($candidates.Count -eq 0) {
            throw "Couldn't auto-detect a LAN IP. Find yours with 'ipconfig' (look for IPv4 Address under your real network adapter) and re-run with -LanIP `"x.x.x.x`"."
        } elseif ($candidates.Count -gt 1) {
            Write-Host "Found more than one possible LAN IP: $($candidates -join ', ')" -ForegroundColor Yellow
            Write-Host "Using the first one: $($candidates[0])  (if that's wrong, re-run with -LanIP `"correct.ip.here`")" -ForegroundColor Yellow
            $LanIP = $candidates[0]
        } else {
            $LanIP = $candidates[0]
        }
    }
    Write-Host "Using LAN IP: $LanIP"

    # ---------------------------------------------------------------------
    Write-Step "Setting up hosts file entry ($Hostname -> $LanIP)"
    # GMod's DHTML browser appears to block URLs that are literally a raw
    # IP address (this seems to affect more than just 127.0.0.1 - possibly
    # any bare IP, as an anti-SSRF measure). A hostname that isn't shaped
    # like a bare IP sidesteps this even when it resolves to the same
    # address, hence pointing $Hostname at $LanIP here.
    $isAdmin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltinRole]::Administrator)
    $hostsPath = "$env:windir\System32\drivers\etc\hosts"
    $marker = "# cloudbox-local-test"
    $hostsLine = "$LanIP`t$Hostname`t$marker"

    if ($isAdmin) {
        $existing = @(Get-Content $hostsPath -ErrorAction SilentlyContinue | Where-Object { $_ -notmatch [regex]::Escape($marker) })
        Set-Content -Path $hostsPath -Value ($existing + $hostsLine) -Force
        Write-Host "hosts file updated: $hostsLine"
    } else {
        Write-Host "Not running as Administrator, so I can't edit the hosts file automatically." -ForegroundColor Yellow
        Write-Host "Add this line yourself: open Notepad AS ADMINISTRATOR, open" -ForegroundColor Yellow
        Write-Host "  $hostsPath" -ForegroundColor Yellow
        Write-Host "and add this line at the end, then save:" -ForegroundColor Yellow
        Write-Host "  $hostsLine" -ForegroundColor White
        Write-Host "(Re-run this script as Administrator to have it done automatically instead - right-click PowerShell, 'Run as administrator'.)" -ForegroundColor Yellow
    }

    # ---------------------------------------------------------------------
    Write-Step "Locating mysql.exe (from your MariaDB install)"
    $mysqlCmd = Get-Command mysql.exe -ErrorAction SilentlyContinue
    if ($mysqlCmd) {
        $mysqlExe = $mysqlCmd.Source
    } else {
        $candidates = Get-ChildItem "C:\Program Files\MariaDB*\bin\mysql.exe", "C:\Program Files\MySQL\MySQL Server*\bin\mysql.exe" -ErrorAction SilentlyContinue
        if ($candidates) {
            $mysqlExe = $candidates[0].FullName
        } else {
            throw "Couldn't find mysql.exe anywhere. Install MariaDB (https://mariadb.org/download/), or tell me where it installed to and I'll adjust this script."
        }
    }
    Write-Host "Using: $mysqlExe"

    if ($MySqlRootPassword -eq "") {
        $securePw = Read-Host "Enter your MariaDB root password (set during install)" -AsSecureString
        $MySqlRootPassword = [Runtime.InteropServices.Marshal]::PtrToStringAuto([Runtime.InteropServices.Marshal]::SecureStringToBSTR($securePw))
    }

    # ---------------------------------------------------------------------
    Write-Step "Applying schema.sql and creating the cloudbox DB user"
    # manual equivalent, if this step errors and you want to run it by hand:
    #   & "<path to mysql.exe>" -u root -p --execute "SOURCE C:/path/to/schema.sql;"
    #
    # NOTE: forward slashes below are deliberate, not a typo - the mysql
    # client has a real, documented bug where Windows backslash paths in a
    # "source" command can produce bogus "You have an error in your SQL
    # syntax" errors (backslash is its escape character). Forward slashes
    # work fine on Windows too.
    $schemaPathForward = "$root\schema.sql" -replace '\\', '/'
    & $mysqlExe -u root "--password=$MySqlRootPassword" --execute "SOURCE $schemaPathForward;" 2>&1 | ForEach-Object { Write-Host $_ }
    if ($LASTEXITCODE -ne 0) { throw "mysql exited with an error applying schema.sql (see above) - likely a wrong root password." }

    # DROP + fresh CREATE rather than CREATE-IF-NOT-EXISTS + ALTER: testing
    # showed ALTER USER can silently fail to fully take effect if the
    # account's gotten into a stuck state from earlier runs, while a clean
    # drop-and-recreate reliably works every time
    $userSql = "DROP USER IF EXISTS 'cloudbox'@'%', 'cloudbox'@'localhost', 'cloudbox'@'127.0.0.1'; "
    foreach ($h in @("%", "localhost", "127.0.0.1")) {
        $userSql += "CREATE USER 'cloudbox'@'$h' IDENTIFIED BY '$DbPassword'; GRANT ALL PRIVILEGES ON cloudbox.* TO 'cloudbox'@'$h'; "
    }
    $userSql += "FLUSH PRIVILEGES;"
    & $mysqlExe -u root "--password=$MySqlRootPassword" --execute $userSql 2>&1 | ForEach-Object { Write-Host $_ }
    if ($LASTEXITCODE -ne 0) { throw "mysql exited with an error creating the cloudbox user (see above)." }
    Write-Host "Database ready."

    # ---------------------------------------------------------------------
    Write-Step "Finding MinIO (local S3 stand-in)"
    # use whatever minio*.exe is already sitting in this folder (e.g. if you
    # downloaded it yourself already, under whatever release-versioned name
    # it came as) before trying to download a fresh one
    $minioExisting = Get-ChildItem "$root\minio*.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($minioExisting) {
        $minioExe = $minioExisting.FullName
        Write-Host "Found existing MinIO binary: $minioExe"
    } else {
        $minioExe = "$root\minio.exe"
        Write-Host "Downloading MinIO..."
        try {
            Invoke-WebRequest -Uri "https://dl.min.io/server/minio/release/windows-amd64/minio.exe" -OutFile $minioExe
        } catch {
            throw "Couldn't download MinIO automatically ($($_.Exception.Message)). Download it yourself from https://dl.min.io/server/minio/release/windows-amd64/minio.exe, drop the .exe into this folder ($root), and run this script again - it'll find it."
        }
    }

    Write-Step "Starting MinIO"
    # MinIO is a server - it's meant to be run from a console with arguments,
    # not double-clicked (double-clicking with no arguments is exactly what
    # was giving the "run through console" message). This starts it for you
    # in its own window, which will just show its logs and stay open - that
    # window staying open with logs scrolling is normal, not an error.
    $env:MINIO_ROOT_USER = "minioadmin"
    $env:MINIO_ROOT_PASSWORD = "minioadmin123"
    New-Item -ItemType Directory -Force -Path "$root\minio-data" | Out-Null
    Start-Process -FilePath $minioExe -ArgumentList "server", "$root\minio-data", "--console-address", ":9001" -WindowStyle Normal

    Write-Host "Waiting for MinIO to come up..."
    $ready = $false
    for ($i = 0; $i -lt 20; $i++) {
        Start-Sleep -Seconds 1
        try {
            Invoke-WebRequest -Uri "http://127.0.0.1:9000/minio/health/live" -UseBasicParsing -TimeoutSec 2 | Out-Null
            $ready = $true
            break
        } catch { }
    }

    $bucketsReady = $false
    if (-not $ready) {
        Write-Host "MinIO didn't respond in time - check the MinIO window that opened for an error message. Continuing anyway; you can create the buckets by hand (see below) once it's up." -ForegroundColor Yellow
    } else {
        Write-Step "Creating the two S3 buckets the backend expects"
        $mcExisting = Get-ChildItem "$root\mc*.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($mcExisting) {
            $mcExe = $mcExisting.FullName
        } else {
            $mcExe = "$root\mc.exe"
            try {
                Invoke-WebRequest -Uri "https://dl.min.io/client/mc/release/windows-amd64/mc.exe" -OutFile $mcExe
            } catch {
                $mcExe = $null
            }
        }

        if ($mcExe) {
            & $mcExe alias set local http://127.0.0.1:9000 minioadmin minioadmin123 | Out-Null
            & $mcExe mb local/flatgrass-toybox-content 2>$null
            & $mcExe mb local/flatgrass-toybox-image 2>$null
            Write-Host "Buckets ready."
            $bucketsReady = $true
        }
    }

    if (-not $bucketsReady) {
        Write-Host ""
        Write-Host "Couldn't create the buckets automatically. Do it by hand (takes 30 seconds):" -ForegroundColor Yellow
        Write-Host "  1. Open http://127.0.0.1:9001 in your browser"
        Write-Host "  2. Log in: minioadmin / minioadmin123"
        Write-Host "  3. Click 'Create Bucket', name it: flatgrass-toybox-content"
        Write-Host "  4. Click 'Create Bucket' again, name it: flatgrass-toybox-image"
        Write-Host ""
    }

    # ---------------------------------------------------------------------
    Write-Step "Building cloudbox-backend.exe"
    Push-Location "$root\cloudbox-backend"
    go build -o cloudbox-backend.exe .
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "go build failed for cloudbox-backend (see above)." }
    Pop-Location

    Write-Step "Building forecaster.exe"
    Push-Location "$root\forecaster"
    go build -o forecaster.exe .
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "go build failed for forecaster (see above)." }
    Pop-Location

    # ---------------------------------------------------------------------
    Write-Step "Launching cloudbox-backend"
    if ($SteamApiKey -eq "") {
        Write-Host "No -SteamApiKey given - the backend will still start, but Steam login on the upload page won't complete until you add one (https://steamcommunity.com/dev/apikey) and restart it." -ForegroundColor Yellow
    }

    $env:AWS_ACCESS_KEY_ID = "minioadmin"
    $env:AWS_SECRET_ACCESS_KEY = "minioadmin123"
    $env:AWS_REGION = "us-east-1"

    # launched via a small companion .ps1 (not the .exe directly) so that if
    # the process crashes or exits immediately (e.g. port already in use),
    # the window pauses to show why instead of just vanishing - this backend
    # has no "listening on ..." success message at all, so silence alone
    # isn't a reliable signal either way
    #
    # NOTE: Start-Process -ArgumentList rejects any array containing an
    # empty string, so -ApiKey is only added below when one was actually
    # given - passing "" directly would throw a validation error
    $backendLauncherArgs = @(
        "-NoProfile",
        "-ExecutionPolicy", "Bypass",
        "-File", "$root\launch-backend.ps1",
        "-WorkDir", "$root\cloudbox-backend",
        "-DbPassword", $DbPassword,
        "-S3Endpoint", "http://127.0.0.1:9000",
        "-BindAddr", "0.0.0.0:8090"
    )
    if ($SteamApiKey -ne "") {
        $backendLauncherArgs += @("-ApiKey", $SteamApiKey)
    }
    Start-Process -FilePath "powershell.exe" -ArgumentList $backendLauncherArgs

    # ---------------------------------------------------------------------
    Write-Step "Setting up a public tunnel for the backend"
    # confirmed by testing: GMod's server-side http.Fetch (used by the
    # in-game client to actually request package info/downloads) hard-blocks
    # ALL private network addresses at the engine level - "Requests to local
    # resources are not allowed" - regardless of hostname tricks (unlike the
    # DHTML browser panel, which only checks the literal URL text and is
    # satisfied by the hosts-file hostname). The only way around this is a
    # real public URL. Using Cloudflare's free, anonymous "quick tunnel" -
    # no account needed, unlike ngrok's heavily restricted 2026 free tier.
    $cloudflaredExisting = Get-ChildItem "$root\cloudflared*.exe" -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($cloudflaredExisting) {
        $cloudflaredExe = $cloudflaredExisting.FullName
        Write-Host "Found existing cloudflared binary: $cloudflaredExe"
    } else {
        $cloudflaredExe = "$root\cloudflared.exe"
        Write-Host "Downloading cloudflared..."
        try {
            Invoke-WebRequest -Uri "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe" -OutFile $cloudflaredExe
        } catch {
            $cloudflaredExe = $null
        }
    }

    $tunnelUrl = $null
    if ($cloudflaredExe) {
        $tunnelLog = "$root\cloudflared.log"
        Remove-Item $tunnelLog -ErrorAction SilentlyContinue
        Start-Process -FilePath $cloudflaredExe -ArgumentList "tunnel", "--url", "http://localhost:8090" -WindowStyle Hidden -RedirectStandardError $tunnelLog

        Write-Host "Waiting for the tunnel URL..."
        for ($i = 0; $i -lt 20; $i++) {
            Start-Sleep -Seconds 1
            if (Test-Path $tunnelLog) {
                $match = Select-String -Path $tunnelLog -Pattern "https://[a-zA-Z0-9-]+\.trycloudflare\.com" | Select-Object -First 1
                if ($match) {
                    $tunnelUrl = $match.Matches[0].Value
                    break
                }
            }
        }
    }

    if ($tunnelUrl) {
        Write-Host "Tunnel ready: $tunnelUrl"
    } else {
        Write-Host "Couldn't set up the tunnel automatically." -ForegroundColor Yellow
        Write-Host "Download cloudflared yourself from:" -ForegroundColor Yellow
        Write-Host "  https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-windows-amd64.exe" -ForegroundColor Yellow
        Write-Host "then run: .\cloudflared.exe tunnel --url http://localhost:8090" -ForegroundColor Yellow
        Write-Host "and use the https://....trycloudflare.com URL it prints for cloudbox_api_url below." -ForegroundColor Yellow
    }

    Write-Step "Launching forecaster (the browsable web UI)"
    $forecasterLauncherArgs = @(
        "-NoProfile",
        "-ExecutionPolicy", "Bypass",
        "-File", "$root\launch-forecaster.ps1",
        "-WorkDir", "$root\forecaster",
        "-ApiUrl", "http://${LanIP}:8090",
        "-BindAddr", "0.0.0.0:8091"
    )
    Start-Process -FilePath "powershell.exe" -ArgumentList $forecasterLauncherArgs

    Start-Sleep -Seconds 2

    $apiUrlForGmod = if ($tunnelUrl) { $tunnelUrl } else { "http://${Hostname}:8090  <-- tunnel setup failed, this WON'T work for http.Fetch, see above" }

    Write-Host ""
    Write-Host "===================================================================" -ForegroundColor Green
    Write-Host " Upload page (from this PC): http://127.0.0.1:8090/addons/upload" -ForegroundColor Green
    Write-Host " Browse addons (this PC):    http://127.0.0.1:8091/browse/addons" -ForegroundColor Green
    Write-Host " MinIO console:              http://127.0.0.1:9001  (minioadmin / minioadmin123)" -ForegroundColor Green
    Write-Host "===================================================================" -ForegroundColor Green
    Write-Host ""
    Write-Host "In GMod's console, run exactly this (quotes matter - the console" -ForegroundColor Yellow
    Write-Host "treats // as a comment without them):" -ForegroundColor Yellow
    Write-Host ""
    Write-Host "  cloudbox_api_url `"$apiUrlForGmod`"" -ForegroundColor White
    Write-Host "  cloudbox_url `"http://${Hostname}:8091`"" -ForegroundColor White
    Write-Host "  cloudbox_reload" -ForegroundColor White
    Write-Host ""
    Write-Host "NOTE: cloudbox_api_url uses a Cloudflare tunnel URL, not the hostname -" -ForegroundColor Yellow
    Write-Host "GMod's http.Fetch blocks ALL private network addresses at the engine" -ForegroundColor Yellow
    Write-Host "level (confirmed by testing), even via the hostname trick that works" -ForegroundColor Yellow
    Write-Host "fine for cloudbox_url's DHTML panel. This tunnel URL changes every time" -ForegroundColor Yellow
    Write-Host "this script runs - re-run it (or restart cloudflared.exe manually) and" -ForegroundColor Yellow
    Write-Host "update cloudbox_api_url again whenever you come back to test." -ForegroundColor Yellow
    Write-Host ""
    Write-Host "Three new windows opened (cloudbox-backend.exe + forecaster.exe + cloudflared, the last one hidden in the background) - leave them running."

    Pause-Exit 0
} catch {
    Write-Host ""
    Write-Host "SOMETHING WENT WRONG:" -ForegroundColor Red
    Write-Host $_.Exception.Message -ForegroundColor Red
    Write-Host ""
    Write-Host "(Full error detail below, in case it's useful to share back:)"
    Write-Host $_.InvocationInfo.PositionMessage
    Pause-Exit 1
}
