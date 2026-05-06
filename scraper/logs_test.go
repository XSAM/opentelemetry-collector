// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package scraper

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/pdata/plog"
)

func TestNewLogs(t *testing.T) {
	mp, err := NewLogs(newTestScrapeLogsFunc(nil))
	require.NoError(t, err)

	require.NoError(t, mp.Start(context.Background(), componenttest.NewNopHost()))
	md, err := mp.ScrapeLogs(context.Background())
	require.NoError(t, err)
	assert.Equal(t, plog.NewLogs(), md)
	require.NoError(t, mp.Shutdown(context.Background()))
}

func TestNewLogs_WithOptions(t *testing.T) {
	want := errors.New("my_error")
	mp, err := NewLogs(newTestScrapeLogsFunc(nil),
		WithStart(func(context.Context, component.Host) error { return want }),
		WithShutdown(func(context.Context) error { return want }),
		WithReload(func(context.Context, component.Config) error { return want }))
	require.NoError(t, err)

	assert.Equal(t, want, mp.Start(context.Background(), componenttest.NewNopHost()))
	assert.Equal(t, want, mp.Shutdown(context.Background()))
	r, ok := mp.(component.Reloader)
	require.True(t, ok)
	assert.Equal(t, want, r.Reload(context.Background(), nil))
}

func TestNewLogs_Reloader(t *testing.T) {
	mp, err := NewLogs(newTestScrapeLogsFunc(nil),
		WithReload(func(context.Context, component.Config) error { return nil }))
	require.NoError(t, err)

	r, ok := mp.(component.Reloader)
	require.True(t, ok)
	require.NoError(t, r.Reload(context.Background(), nil))
}

func TestNewLogs_ReloadNilFunc(t *testing.T) {
	mp, err := NewLogs(newTestScrapeLogsFunc(nil))
	require.NoError(t, err)

	r, ok := mp.(component.Reloader)
	require.True(t, ok)
	require.NoError(t, r.Reload(context.Background(), nil))
}

func TestNewLogs_NilRequiredFields(t *testing.T) {
	_, err := NewLogs(nil)
	require.Error(t, err)
}

func TestNewLogs_ProcessLogsError(t *testing.T) {
	want := errors.New("my_error")
	mp, err := NewLogs(newTestScrapeLogsFunc(want))
	require.NoError(t, err)
	_, err = mp.ScrapeLogs(context.Background())
	require.ErrorIs(t, err, want)
}

func TestLogsConcurrency(t *testing.T) {
	mp, err := NewLogs(newTestScrapeLogsFunc(nil))
	require.NoError(t, err)
	require.NoError(t, mp.Start(context.Background(), componenttest.NewNopHost()))

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			for range 10000 {
				_, errScrape := mp.ScrapeLogs(context.Background())
				assert.NoError(t, errScrape)
			}
		})
	}
	wg.Wait()
	require.NoError(t, mp.Shutdown(context.Background()))
}

func newTestScrapeLogsFunc(retError error) ScrapeLogsFunc {
	return func(_ context.Context) (plog.Logs, error) {
		return plog.NewLogs(), retError
	}
}
