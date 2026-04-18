package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type DownloadStatus string

const (
	StatusQueued      DownloadStatus = "Queued"
	StatusDownloading DownloadStatus = "Downloading"
	StatusPaused      DownloadStatus = "Paused"
	StatusCompleted   DownloadStatus = "Completed"
	StatusError       DownloadStatus = "Error"
)

type Download struct {
	ID          string         `json:"id"`
	URL         string         `json:"url"`
	Filename    string         `json:"filename"`
	SavePath    string         `json:"savePath"`
	TotalSize   int64          `json:"totalSize"`
	Downloaded  int64          `json:"downloaded"`
	Speed       float64        `json:"speed"`
	Status      DownloadStatus `json:"status"`
	Connections       int       `json:"connections"`
	ActiveConnections int       `json:"activeConnections"`
	ConnectionSpeeds  []float64 `json:"connectionSpeeds,omitempty"`
	Progress          float64   `json:"progress"`
	ETA         string         `json:"eta"`
	Error       string         `json:"error,omitempty"`
	StartedAt   time.Time      `json:"startedAt"`
	CompletedAt *time.Time     `json:"completedAt,omitempty"`
}

type Settings struct {
	DefaultSavePath   string  `json:"defaultSavePath"`
	MaxConnections    int     `json:"maxConnections"`
	MaxDownloads      int     `json:"maxDownloads"`
	SpeedLimitEnabled bool    `json:"speedLimitEnabled"`
	SpeedLimitMBps    float64 `json:"speedLimitMBps"`
	Theme             string  `json:"theme"`
}

type StartDownloadRequest struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	SavePath    string `json:"savePath"`
	Connections int    `json:"connections"`
}

type App struct {
	ctx          context.Context
	downloads    map[string]*Download
	mu           sync.RWMutex
	settings     Settings
	cancelMap    map[string]context.CancelFunc
	settingsPath string
	sessionPath  string
}

func NewApp() *App {
	homeDir, _ := os.UserHomeDir()
	defaultSave := filepath.Join(homeDir, "Downloads", "GoLoadix")
	settingsDir := filepath.Join(homeDir, ".goloadix")
	_ = os.MkdirAll(settingsDir, 0755)
	_ = os.MkdirAll(defaultSave, 0755)

	app := &App{
		downloads: make(map[string]*Download),
		cancelMap: make(map[string]context.CancelFunc),
		settingsPath: filepath.Join(settingsDir, "settings.json"),
		sessionPath:  filepath.Join(settingsDir, "session.json"),
		settings: Settings{
			DefaultSavePath:   defaultSave,
			MaxConnections:    16,
			MaxDownloads:      1,
			SpeedLimitEnabled: false,
			SpeedLimitMBps:    0,
			Theme:             "dark",
		},
	}
	app.loadSettings()
	return app
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.loadSession()
	go a.progressBroadcaster()
	go a.sessionSaver()
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	for _, cancel := range a.cancelMap {
		cancel()
	}
	a.mu.Unlock()
	a.saveSession()
}

func (a *App) saveSession() {
	a.mu.RLock()
	list := make([]*Download, 0, len(a.downloads))
	for _, d := range a.downloads {
		cp := *d
		if cp.Status == StatusDownloading || cp.Status == StatusQueued {
			cp.Status = StatusPaused
		}
		cp.Speed = 0
		cp.ActiveConnections = 0
		cp.ConnectionSpeeds = nil
		cp.ETA = ""
		list = append(list, &cp)
	}
	a.mu.RUnlock()

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(a.sessionPath, data, 0644)
}

func (a *App) loadSession() {
	data, err := os.ReadFile(a.sessionPath)
	if err != nil {
		return
	}
	var list []*Download
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	a.mu.Lock()
	for _, d := range list {
		if d.Status == StatusDownloading || d.Status == StatusQueued {
			d.Status = StatusPaused
		}
		d.Speed = 0
		d.ActiveConnections = 0
		d.ConnectionSpeeds = nil
		a.downloads[d.ID] = d
	}
	a.mu.Unlock()
}

func (a *App) sessionSaver() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.saveSession()
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *App) progressBroadcaster() {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			a.mu.RLock()
			downloads := make([]*Download, 0, len(a.downloads))
			for _, d := range a.downloads {
				cp := *d
				downloads = append(downloads, &cp)
			}
			a.mu.RUnlock()
			wailsruntime.EventsEmit(a.ctx, "downloads:update", downloads)
		case <-a.ctx.Done():
			return
		}
	}
}

func (a *App) GetDownloads() []*Download {
	a.mu.RLock()
	defer a.mu.RUnlock()
	result := make([]*Download, 0, len(a.downloads))
	for _, d := range a.downloads {
		cp := *d
		result = append(result, &cp)
	}
	return result
}

func (a *App) GetSettings() Settings {
	return a.settings
}

func (a *App) SaveSettings(s Settings) error {
	a.settings = s
	return a.persistSettings()
}

func (a *App) BrowseFolder() string {
	dir, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:            "Select Download Folder",
		DefaultDirectory: a.settings.DefaultSavePath,
	})
	if err != nil || dir == "" {
		return a.settings.DefaultSavePath
	}
	return dir
}

func (a *App) OpenFolder(path string) {
	dir := path
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		dir = filepath.Dir(path)
	}
	_ = exec.Command("explorer", dir).Start()
}

func (a *App) StartDownload(req StartDownloadRequest) (*Download, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("URL is required")
	}
	parsedURL, err := url.Parse(req.URL)
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
		return nil, fmt.Errorf("invalid URL: must start with http:// or https://")
	}

	savePath := req.SavePath
	if savePath == "" {
		savePath = a.settings.DefaultSavePath
	}
	_ = os.MkdirAll(savePath, 0755)

	filename := req.Filename
	if filename == "" {
		filename = filepath.Base(parsedURL.Path)
		if filename == "" || filename == "." || filename == "/" {
			filename = "download_" + strconv.FormatInt(time.Now().Unix(), 10)
		}
	}

	connections := req.Connections
	if connections <= 0 {
		connections = a.settings.MaxConnections
	}
	if connections > 128 {
		connections = 128
	}
	if connections < 1 {
		connections = 1
	}

	id := strconv.FormatInt(time.Now().UnixNano(), 10)
	d := &Download{
		ID:          id,
		URL:         req.URL,
		Filename:    filename,
		SavePath:    filepath.Join(savePath, filename),
		Status:      StatusQueued,
		Connections: connections,
		StartedAt:   time.Now(),
	}

	a.mu.Lock()
	a.downloads[id] = d
	a.mu.Unlock()

	go a.saveSession()
	go a.runDownload(d)
	return d, nil
}

func (a *App) PauseDownload(id string) error {
	a.mu.Lock()
	d, ok := a.downloads[id]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("download not found")
	}
	if d.Status != StatusDownloading {
		a.mu.Unlock()
		return nil
	}
	d.Status = StatusPaused
	cancel, hasCancel := a.cancelMap[id]
	a.mu.Unlock()

	if hasCancel {
		cancel()
	}
	return nil
}

func (a *App) ResumeDownload(id string) error {
	a.mu.RLock()
	d, ok := a.downloads[id]
	a.mu.RUnlock()
	if !ok {
		return fmt.Errorf("download not found")
	}
	if d.Status != StatusPaused && d.Status != StatusError {
		return nil
	}
	d.Status = StatusQueued
	go a.runDownload(d)
	return nil
}

func (a *App) CancelDownload(id string) error {
	a.mu.Lock()
	d, ok := a.downloads[id]
	if !ok {
		a.mu.Unlock()
		return fmt.Errorf("download not found")
	}
	cancel, hasCancel := a.cancelMap[id]
	delete(a.downloads, id)
	delete(a.cancelMap, id)
	a.mu.Unlock()

	if hasCancel {
		cancel()
	}
	_ = os.Remove(d.SavePath)
	go a.saveSession()
	return nil
}

func (a *App) runDownload(d *Download) {
	ctx, cancel := context.WithCancel(a.ctx)

	a.mu.Lock()
	a.cancelMap[d.ID] = cancel
	d.Status = StatusDownloading
	d.Error = ""
	a.mu.Unlock()

	defer cancel()

	if err := a.download(ctx, d); err != nil {
		a.mu.Lock()
		if d.Status == StatusDownloading {
			d.Status = StatusError
			d.Error = err.Error()
			d.Speed = 0
		}
		a.mu.Unlock()
	}
}

func (a *App) download(ctx context.Context, d *Download) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, d.URL, nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()

	totalSize := resp.ContentLength
	supportsRange := resp.Header.Get("Accept-Ranges") == "bytes" && totalSize > 0

	a.mu.Lock()
	d.TotalSize = totalSize
	a.mu.Unlock()

	if !supportsRange || d.Connections <= 1 || totalSize <= 0 {
		return a.downloadSingle(ctx, d)
	}
	return a.downloadMultipart(ctx, d, totalSize)
}

func (a *App) downloadSingle(ctx context.Context, d *Download) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return err
	}

	var startByte int64
	if info, statErr := os.Stat(d.SavePath); statErr == nil {
		startByte = info.Size()
		if startByte > 0 {
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
		}
	}

	httpClient := makeParallelClient(1)
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	a.mu.Lock()
	d.ActiveConnections = 1
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		d.ActiveConnections = 0
		a.mu.Unlock()
	}()

	flags := os.O_CREATE | os.O_WRONLY
	if startByte > 0 && resp.StatusCode == http.StatusPartialContent {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		startByte = 0
	}

	f, err := os.OpenFile(d.SavePath, flags, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	var downloaded int64 = startByte
	buf := make([]byte, 32*1024)
	lastUpdate := time.Now()
	lastBytes := startByte

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := f.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(n)
			now := time.Now()
			elapsed := now.Sub(lastUpdate).Seconds()
			if elapsed >= 0.5 {
				speed := float64(downloaded-lastBytes) / elapsed
				a.mu.Lock()
				d.Downloaded = downloaded
				d.Speed = speed
				d.ConnectionSpeeds = []float64{speed}
				if d.TotalSize > 0 {
					d.Progress = float64(downloaded) / float64(d.TotalSize) * 100
					d.ETA = formatETA(float64(d.TotalSize-downloaded) / math.Max(speed, 1))
				}
				a.mu.Unlock()
				lastUpdate = now
				lastBytes = downloaded
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	now := time.Now()
	a.mu.Lock()
	d.Downloaded = downloaded
	d.Progress = 100
	d.Speed = 0
	d.Status = StatusCompleted
	d.ETA = ""
	d.CompletedAt = &now
	a.mu.Unlock()
	return nil
}

func (a *App) downloadMultipart(ctx context.Context, d *Download, totalSize int64) error {
	numConns := d.Connections
	baseChunk := totalSize / int64(numConns)
	cacheFile := d.SavePath + ".goloadix"

	// Build chunk list, resuming from sidecar when available
	chunks := make([]ChunkProgress, numConns)
	for i := 0; i < numConns; i++ {
		chunks[i] = ChunkProgress{
			Index: i,
			Start: int64(i) * baseChunk,
			End:   int64(i+1)*baseChunk - 1,
		}
		if i == numConns-1 {
			chunks[i].End = totalSize - 1
		}
	}
	if cache, err := loadDownloadCache(cacheFile); err == nil &&
		cache.TotalSize == totalSize && len(cache.Chunks) == numConns {
		for i, c := range cache.Chunks {
			chunks[i].Downloaded = c.Downloaded
		}
	}

	// Per-chunk atomic progress counters
	chunkDone := make([]int64, numConns)
	var alreadyDone int64
	for i, c := range chunks {
		chunkDone[i] = c.Downloaded
		alreadyDone += c.Downloaded
	}
	var totalDownloaded int64 = alreadyDone

	// Open and pre-allocate the output file once
	f, err := os.OpenFile(d.SavePath, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	if info, _ := f.Stat(); info.Size() != totalSize {
		if err := f.Truncate(totalSize); err != nil {
			return err
		}
	}

	a.mu.Lock()
	d.Downloaded = alreadyDone
	if totalSize > 0 {
		d.Progress = float64(alreadyDone) / float64(totalSize) * 100
	}
	a.mu.Unlock()

	// workerCtx controls background helpers; cancelled after download goroutines finish
	workerCtx, workerCancel := context.WithCancel(ctx)

	var bgWg sync.WaitGroup

	// Speed & progress updater — reads atomic counters every 500ms
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		t := time.NewTicker(500 * time.Millisecond)
		defer t.Stop()
		last := atomic.LoadInt64(&totalDownloaded)
		lastChunk := make([]int64, numConns)
		for i := range chunkDone {
			lastChunk[i] = atomic.LoadInt64(&chunkDone[i])
		}
		lastTime := time.Now()
		for {
			select {
			case <-t.C:
				cur := atomic.LoadInt64(&totalDownloaded)
				now := time.Now()
				elapsed := math.Max(now.Sub(lastTime).Seconds(), 0.001)
				speed := float64(cur-last) / elapsed

				connSpeeds := make([]float64, numConns)
				for i := range chunkDone {
					curChunk := atomic.LoadInt64(&chunkDone[i])
					connSpeeds[i] = float64(curChunk-lastChunk[i]) / elapsed
					lastChunk[i] = curChunk
				}

				a.mu.Lock()
				d.Downloaded = cur
				d.Speed = speed
				d.ConnectionSpeeds = connSpeeds
				if totalSize > 0 {
					d.Progress = float64(cur) / float64(totalSize) * 100
					d.ETA = formatETA(float64(totalSize-cur) / math.Max(speed, 1))
				}
				a.mu.Unlock()
				last = cur
				lastTime = now
			case <-workerCtx.Done():
				return
			}
		}
	}()

	// Sidecar writer — persists per-chunk progress every 2s and on exit
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		writeSidecar := func() {
			c := &DownloadCache{
				URL: d.URL, TotalSize: totalSize,
				Connections: numConns,
				Chunks:      make([]ChunkProgress, numConns),
			}
			for i := range chunks {
				c.Chunks[i] = ChunkProgress{
					Index:      chunks[i].Index,
					Start:      chunks[i].Start,
					End:        chunks[i].End,
					Downloaded: atomic.LoadInt64(&chunkDone[i]),
				}
			}
			_ = saveDownloadCache(cacheFile, c)
		}
		for {
			select {
			case <-t.C:
				writeSidecar()
			case <-workerCtx.Done():
				writeSidecar()
				return
			}
		}
	}()

	// Download goroutines — each writes directly to its byte range in the file
	sharedClient := makeParallelClient(numConns)
	var dlWg sync.WaitGroup
	var activeConns int64
	errs := make([]error, numConns)

	for i := range chunks {
		chunkLen := chunks[i].End - chunks[i].Start + 1
		if atomic.LoadInt64(&chunkDone[i]) >= chunkLen {
			continue // already complete, skip
		}
		dlWg.Add(1)
		go func(idx int) {
			defer dlWg.Done()
			chunk := &chunks[idx]
			resumeFrom := chunk.Start + atomic.LoadInt64(&chunkDone[idx])

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
			if err != nil {
				errs[idx] = err
				return
			}
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", resumeFrom, chunk.End))

			resp, err := sharedClient.Do(req)
			if err != nil {
				errs[idx] = err
				return
			}
			defer resp.Body.Close()

			active := atomic.AddInt64(&activeConns, 1)
			a.mu.Lock()
			d.ActiveConnections = int(active)
			a.mu.Unlock()
			defer func() {
				c := atomic.AddInt64(&activeConns, -1)
				a.mu.Lock()
				d.ActiveConnections = int(c)
				a.mu.Unlock()
			}()

			writeAt := resumeFrom
			buf := make([]byte, 64*1024)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					if _, werr := f.WriteAt(buf[:n], writeAt); werr != nil {
						errs[idx] = werr
						return
					}
					writeAt += int64(n)
					atomic.AddInt64(&chunkDone[idx], int64(n))
					atomic.AddInt64(&totalDownloaded, int64(n))
				}
				if readErr == io.EOF {
					break
				}
				if readErr != nil {
					errs[idx] = readErr
					return
				}
			}
		}(i)
	}

	dlWg.Wait()
	workerCancel()  // stop helpers and trigger final sidecar write
	bgWg.Wait()     // ensure sidecar is fully written before we act on it

	// Context was cancelled (user paused) — keep sidecar for resume
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	// All chunks complete — remove sidecar
	_ = os.Remove(cacheFile)

	now := time.Now()
	a.mu.Lock()
	d.Downloaded = totalSize
	d.Progress = 100
	d.Speed = 0
	d.Status = StatusCompleted
	d.ETA = ""
	d.CompletedAt = &now
	a.mu.Unlock()
	return nil
}

func (a *App) GetDiskSpace() string {
	a.mu.RLock()
	path := a.settings.DefaultSavePath
	a.mu.RUnlock()
	return getDiskFreeSpace(path)
}

func (a *App) GetFileIcon(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".mp4", ".mkv", ".avi", ".mov", ".wmv", ".webm":
		return "movie"
	case ".mp3", ".flac", ".wav", ".aac", ".ogg":
		return "audio_file"
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".bz2":
		return "folder_zip"
	case ".pdf":
		return "picture_as_pdf"
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp", ".svg":
		return "image"
	case ".exe", ".msi", ".deb", ".rpm", ".dmg", ".pkg":
		return "terminal"
	case ".iso", ".img":
		return "album"
	default:
		return "description"
	}
}

func makeParallelClient(numConns int) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSNextProto:        make(map[string]func(string, *tls.Conn) http.RoundTripper),
			MaxConnsPerHost:     0,
			MaxIdleConnsPerHost: numConns,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

func formatETA(seconds float64) string {
	if seconds <= 0 || math.IsInf(seconds, 0) || math.IsNaN(seconds) {
		return "--"
	}
	s := int(seconds)
	if s < 60 {
		return fmt.Sprintf("%d secs", s)
	} else if s < 3600 {
		return fmt.Sprintf("%d mins", s/60)
	}
	return fmt.Sprintf("%dh %dm", s/3600, (s%3600)/60)
}

func (a *App) loadSettings() {
	data, err := os.ReadFile(a.settingsPath)
	if err != nil {
		return
	}
	_ = json.Unmarshal(data, &a.settings)
}

func (a *App) persistSettings() error {
	data, err := json.MarshalIndent(a.settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.settingsPath, data, 0644)
}
