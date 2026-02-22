package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zsiec/prism/test/tools/tsutil"
)

type sourceFile struct {
	name string
	url  string
}

// CC-BY licensed Blender Foundation films for broadcast-realistic test content.
var filmSources = []sourceFile{
	{
		name: "tears_of_steel.mov",
		url:  "http://ftp.nluug.nl/pub/graphics/blender/demo/movies/ToS/ToS-4k-1920.mov",
	},
	{
		name: "sintel.mkv",
		url:  "http://ftp.nluug.nl/pub/graphics/blender/demo/movies/Sintel.2010.1080p.mkv",
	},
	{
		name: "bbb.mov",
		url:  "https://download.blender.org/peach/bigbuckbunny_movies/big_buck_bunny_1080p_h264.mov",
	},
	{
		name: "elephants_dream.mov",
		url:  "https://download.blender.org/ED/elephantsdream-720-h264-st-aac.mov",
	},
}

func downloadSources(dir string) error {
	var needed []sourceFile
	for _, s := range filmSources {
		path := filepath.Join(dir, s.name)
		if tsutil.FileExists(path) {
			info, _ := os.Stat(path)
			if info != nil && info.Size() > 1024 {
				fmt.Printf("  Cached: %s\n", s.name)
				continue
			}
		}
		needed = append(needed, s)
	}

	if len(needed) == 0 {
		fmt.Println("  All sources already cached")
		return nil
	}

	fmt.Printf("  Downloading %d film(s)...\n", len(needed))

	var wg sync.WaitGroup
	errCh := make(chan error, len(needed))
	sem := make(chan struct{}, 2)

	for _, s := range needed {
		wg.Add(1)
		go func(sf sourceFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			finalPath := filepath.Join(dir, sf.name)
			if err := downloadFile(finalPath, sf.url); err != nil {
				errCh <- fmt.Errorf("download %s: %w", sf.name, err)
				return
			}

			info, _ := os.Stat(finalPath)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			fmt.Printf("  Downloaded: %s (%.1f MB)\n", sf.name, float64(size)/1024/1024)
		}(s)
	}

	wg.Wait()
	close(errCh)

	var errs []error
	for err := range errCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

func downloadFile(path, url string) error {
	client := &http.Client{Timeout: 30 * time.Minute}

	fmt.Printf("  Fetching: %s\n", url)
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}

	_, err = io.Copy(f, resp.Body)
	f.Close()
	if err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, path)
}
