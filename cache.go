package main

import (
	"encoding/json"
	"os"
)

type ChunkProgress struct {
	Index      int   `json:"index"`
	Start      int64 `json:"start"`
	End        int64 `json:"end"`
	Downloaded int64 `json:"downloaded"`
}

type DownloadCache struct {
	URL         string          `json:"url"`
	TotalSize   int64           `json:"totalSize"`
	Connections int             `json:"connections"`
	Chunks      []ChunkProgress `json:"chunks"`
}

func loadDownloadCache(path string) (*DownloadCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c DownloadCache
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func saveDownloadCache(path string, c *DownloadCache) error {
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
