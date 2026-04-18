# GoLoadix — Free Open Source Download Manager for Windows

GoLoadix is a fast, lightweight, open source download manager for Windows built with Go and Wails. It accelerates downloads by splitting files into parallel chunks over multiple independent TCP connections — similar to IDM (Internet Download Manager) but completely free, open source, and with no ads.

**Multi-connection HTTP downloads** — splits files into parallel chunks over independent TCP connections to saturate your bandwidth. Pause, resume, and session-persist downloads across restarts.

> Free download accelerator · Multi-threaded downloader · IDM alternative · Open source · No ads · No bloatware

![GoLoadix active download queue](screenshots/Active_Queuejpg.jpg)

---

## Features

- **Multi-part downloads** — up to 128 parallel connections per file via HTTP Range requests; dramatically faster than single-connection downloads
- **True parallel TCP** — HTTP/2 multiplexing disabled; each chunk gets its own connection for maximum throughput
- **Pause & Resume** — per-chunk progress saved to a `.goloadix` sidecar file; survives app restarts
- **Session persistence** — all downloads restored on next launch from `~/.goloadix/session.json`
- **Per-connection speed** — hover any download card to see live speed per connection
- **Real-time progress** — speed, ETA, and progress bar updated every 500 ms
- **Free disk space** — reads from the drive your download folder lives on
- **Dark UI** — Kinetic Vault theme, electric cyan on deep obsidian (`#4cd6fb` / `#0d0d1a`)
- **Portable or installed** — run as a single `.exe` or use the NSIS installer; no runtime dependencies

---

## Download

Head to [Releases](../../releases) and grab the latest Windows build — no runtime, no dependencies:

| File | Description |
|------|-------------|
| `GoLoadix.exe` | Portable — no install needed, just run |
| `GoLoadix-amd64-installer.exe` | NSIS installer — adds Start Menu shortcut |

> **System requirements:** Windows 10/11 (64-bit). No .NET, no Java, no Visual C++ required.

---

## Building from Source

### Prerequisites

| Tool | Version | Install |
|------|---------|---------|
| Go | 1.21+ | https://go.dev/dl |
| Wails CLI | v2.12+ | `go install github.com/wailsapp/wails/v2/cmd/wails@latest` |
| NSIS *(optional, installer only)* | 3.x | `winget install NSIS.NSIS` |

### Build

```bash
# Clone
git clone https://github.com/YOUR_USERNAME/GoLoadix.git
cd GoLoadix

# Portable exe
wails build

# Portable exe + NSIS installer
wails build -nsis
```

Output lands in `build/bin/`.

---

## Project Structure

```
GoLoadix/
├── app.go                   # All backend logic (download engine, settings, session)
├── cache.go                 # Sidecar cache types & I/O (pause/resume state)
├── diskspace_windows.go     # Windows disk free space via GetDiskFreeSpaceEx
├── main.go                  # Wails entry point, window config
├── go.mod / go.sum
├── wails.json
├── build/
│   ├── appicon.png          # 256px app icon
│   └── windows/
│       ├── icon.ico         # Multi-size ICO (16–256px)
│       ├── info.json        # Windows version metadata
│       └── installer/       # NSIS installer scripts
└── frontend/
    ├── index.html           # Full UI (Tailwind CSS, Material Symbols)
    └── src/
        └── main.js          # Frontend logic (rendering, events, settings)
```

---

## How It Works

1. **HEAD request** — checks `Accept-Ranges` and `Content-Length`
2. **File pre-allocation** — `f.Truncate(totalSize)` reserves disk space upfront
3. **Chunk goroutines** — each sends `Range: bytes=N-M` on its own TCP connection (`http.Transport` with HTTP/2 disabled)
4. **Direct writes** — `f.WriteAt(buf, offset)` — no in-memory accumulation
5. **Sidecar cache** — `filename.goloadix` tracks bytes downloaded per chunk every 2 s
6. **Resume** — on restart, reads the sidecar and skips already-downloaded byte ranges

---

## Why GoLoadix?

Most download managers are either paid (IDM), ad-supported, or bundled with bloatware. GoLoadix is a clean, open source alternative that does one thing well: download files as fast as your connection allows, using parallel TCP connections and HTTP Range requests. No browser extension required, no upsells, no tracking.

**Compared to alternatives:**

| Feature | GoLoadix | IDM | Free Download Manager |
|---------|----------|-----|-----------------------|
| Free | ✅ | ❌ (paid) | ✅ |
| Open source | ✅ | ❌ | ❌ |
| No ads | ✅ | ✅ | ⚠️ |
| Parallel connections | ✅ (up to 128) | ✅ | ✅ |
| Pause & Resume | ✅ | ✅ | ✅ |
| Portable exe | ✅ | ❌ | ❌ |

---

## License

MIT — see [LICENSE](LICENSE).

---

*Keywords: open source download manager windows, free download accelerator, IDM alternative free, multi-threaded download manager, parallel download tool, HTTP range download, Go download manager, Wails desktop app*
