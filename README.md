# no-bs-transfer

A lightweight, ultra-fast desktop application designed to solve the friction of transferring files between devices without size limits, third-party cloud uploads, or account logins. By leveraging **Cloudflare Tunnels**, it creates instant, secure end-to-end HTTPS links directly to your machine without opening ports or configuring your router.

---

## Features

- **No size limit:** Stream files of 1 GB, 50 GB, or multiple TBs. Constant $O(1)$ RAM usage thanks to direct disk-to-socket streaming.
- **Two-way transfers (Send & Receive):** Share files with direct links or let guests upload photos, videos, and documents straight to your PC from any browser.
- **Automatic Cloudflare Tunnels:** Generates secure HTTPS links (`https://*.trycloudflare.com`) without opening router ports or registering domain names.
- **Custom domains & named tunnels:** Support for personal domains (e.g., `transfer.yourdomain.com`) and Cloudflare Tunnel tokens with configurable port binding.
- **Local Network (LAN) mode:** Transfer at full speed completely offline with zero internet dependency.
- **Live guest monitoring:**
  - Real-time list of connected devices (iPhone, Android, Windows, Mac, Linux, curl).
  - Download and upload progress (0% to 100%) and transferred bytes.
  - Individual client speeds and total aggregate speed in MB/s.
  - States: Downloading, Uploading, Paused, Completed.
- **Instant QR code:** Scan with any smartphone camera to download or upload without installing any app.
- **Download resumption:** Native support for HTTP `Accept-Ranges: bytes` headers (allows pausing and resuming downloads).
- **Portable mode:** Make the app completely **portable** by placing an empty file named `portable.txt` next to the executable.
- **Minimal resource consumption:** Weighs only **~13 MB** and consumes **~50 MB of RAM**.

---

## Tech Stack

- **Backend:** Go (Golang)
- **GUI:** Wails v2 (Windows WebView2 native)
- **Frontend:** Svelte 5 + TypeScript + Vite + Tailwind CSS + `@lucide/svelte` + `qrcode`
- **Tunnel:** Cloudflare Tunnel CLI (`cloudflared`)

---

## How to Run the Application

### Download (Pre-built Executable)
Download the latest standalone binary directly from [GitHub Releases](https://github.com/tdominguez33/no-bs-transfer/releases)—no installation needed, just run the executable.

### Run from Source

#### Development mode (with Hot-Reload):
```powershell
wails dev
```

#### Build for production:
```powershell
wails build
```

---

## Extra Flags

### Ignore local system installation (`--ignore-local-install`)
Ignores any pre-installed `cloudflared` binary on the system (`PATH`, `Program Files`, `WinGet`, etc.) or next to the executable, and searches **only** inside the AppData folder (`%LOCALAPPDATA%\no-bs-transfer\bin\cloudflared.exe`):

- **If missing in AppData:** The app automatically downloads `cloudflared` directly into the AppData folder.
- **If already downloaded:** Uses the binary located in that folder.

#### How to use:
- **With environment variable in PowerShell:**
  ```powershell
  $env:NO_BS_IGNORE_LOCAL_INSTALL="1"; wails dev
  ```
- **Or passing the CLI argument to the executable:**
  ```powershell
  .\build\bin\no-bs-transfer.exe --ignore-local-install
  ```

### Force HTTP/2 protocol (`--force-http2`)
Forces `cloudflared` to connect to Cloudflare using **HTTP/2 over TCP (port 443)** instead of the default **QUIC (UDP)** protocol, eliminating timeout and fallback delays:

- **Bypasses restrictive firewalls:** Ideal for corporate or restricted networks where non-standard UDP traffic is blocked, establishing the tunnel directly via standard HTTPS.
- **Compatible:** Applies to both automatic quick tunnels (`trycloudflare.com`) and named tunnels with custom tokens and domains.

#### How to use:
- **With environment variable in PowerShell:**
  ```powershell
  $env:NO_BS_FORCE_HTTP2="1"; wails dev
  ```
- **Or passing the CLI argument to the executable:**
  ```powershell
  .\build\bin\no-bs-transfer.exe --force-http2
  ```
