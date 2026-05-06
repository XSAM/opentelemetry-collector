// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

//go:generate mdatagen metadata.yaml

package fileprovider // import "go.opentelemetry.io/collector/confmap/provider/fileprovider"

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"

	"go.opentelemetry.io/collector/confmap"
)

const schemeName = "file"

type fileWatch struct {
	watcher *fsnotify.Watcher
	cancel  context.CancelFunc
}

type provider struct {
	mu      sync.Mutex
	watches map[string]*fileWatch // key: cleaned file path
}

// NewFactory returns a factory for a confmap.Provider that reads the configuration from a file.
//
// This Provider supports "file" scheme, and can be called with a "uri" that follows:
//
//	file-uri		= "file:" local-path
//	local-path		= [ drive-letter ] file-path
//	drive-letter	= ALPHA ":"
//
// The "file-path" can be relative or absolute, and it can be any OS supported format.
//
// Examples:
// `file:path/to/file` - relative path (unix, windows)
// `file:/path/to/file` - absolute path (unix, windows)
// `file:c:/path/to/file` - absolute path including drive-letter (windows)
// `file:c:\path\to\file` - absolute path including drive-letter (windows)
//
// When a non-nil WatcherFunc is supplied, the provider watches the file for
// modifications and invokes the callback on each change. The Resolver uses
// this to re-resolve and deliver an event on Resolver.Watch().
func NewFactory() confmap.ProviderFactory {
	return confmap.NewProviderFactory(newProvider)
}

func newProvider(confmap.ProviderSettings) confmap.Provider {
	return &provider{watches: map[string]*fileWatch{}}
}

func (fmp *provider) Retrieve(_ context.Context, uri string, watcherFunc confmap.WatcherFunc) (*confmap.Retrieved, error) {
	if !strings.HasPrefix(uri, schemeName+":") {
		return nil, fmt.Errorf("%q uri is not supported by %q provider", uri, schemeName)
	}

	// Clean the path before using it.
	path := filepath.Clean(uri[len(schemeName)+1:])
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read the file %v: %w", uri, err)
	}

	// Always stop any prior watch for this path — Retrieve may be called
	// repeatedly by the Resolver (initial + each reload), and we must not
	// leak watchers.
	fmp.stopWatch(path)

	if watcherFunc != nil {
		if err := fmp.startWatch(path, watcherFunc); err != nil {
			return nil, fmt.Errorf("unable to watch the file %v: %w", uri, err)
		}
	}

	return confmap.NewRetrievedFromYAML(content)
}

// startWatch registers an fsnotify watcher on the file's parent directory
// (direct file watches break when editors replace the file via rename) and
// fires watcherFunc on any modification to the target path.
func (fmp *provider) startWatch(path string, watcherFunc confmap.WatcherFunc) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := w.Add(filepath.Dir(path)); err != nil {
		_ = w.Close()
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())

	fmp.mu.Lock()
	fmp.watches[path] = &fileWatch{watcher: w, cancel: cancel}
	fmp.mu.Unlock()

	go func() {
		target := filepath.Clean(path)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != target {
					continue
				}
				if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
					watcherFunc(&confmap.ChangeEvent{})
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				watcherFunc(&confmap.ChangeEvent{Error: err})
			}
		}
	}()
	return nil
}

func (fmp *provider) stopWatch(path string) {
	fmp.mu.Lock()
	fw, ok := fmp.watches[path]
	delete(fmp.watches, path)
	fmp.mu.Unlock()
	if !ok {
		return
	}
	fw.cancel()
	_ = fw.watcher.Close()
}

func (*provider) Scheme() string {
	return schemeName
}

func (fmp *provider) Shutdown(context.Context) error {
	fmp.mu.Lock()
	watches := fmp.watches
	fmp.watches = map[string]*fileWatch{}
	fmp.mu.Unlock()

	var firstErr error
	for _, fw := range watches {
		fw.cancel()
		if err := fw.watcher.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
