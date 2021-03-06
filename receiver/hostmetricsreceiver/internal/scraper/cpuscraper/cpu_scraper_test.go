// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cpuscraper

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/shirou/gopsutil/cpu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.opentelemetry.io/collector/consumer/pdata"
	"go.opentelemetry.io/collector/receiver/hostmetricsreceiver/internal"
)

func TestScrapeMetrics(t *testing.T) {
	type testCase struct {
		name              string
		bootTimeFunc      func() (uint64, error)
		timesFunc         func(bool) ([]cpu.TimesStat, error)
		expectedStartTime pdata.TimestampUnixNano
		initializationErr string
		expectedErr       string
	}

	testCases := []testCase{
		{
			name: "Standard",
		},
		{
			name:              "Validate Start Time",
			bootTimeFunc:      func() (uint64, error) { return 100, nil },
			expectedStartTime: 100 * 1e9,
		},
		{
			name:              "Boot Time Error",
			bootTimeFunc:      func() (uint64, error) { return 0, errors.New("err1") },
			initializationErr: "err1",
		},
		{
			name:        "Times Error",
			timesFunc:   func(bool) ([]cpu.TimesStat, error) { return nil, errors.New("err2") },
			expectedErr: "err2",
		},
	}

	for _, test := range testCases {
		t.Run(test.name, func(t *testing.T) {
			scraper := newCPUScraper(context.Background(), &Config{})
			if test.bootTimeFunc != nil {
				scraper.bootTime = test.bootTimeFunc
			}
			if test.timesFunc != nil {
				scraper.times = test.timesFunc
			}

			err := scraper.Initialize(context.Background())
			if test.initializationErr != "" {
				assert.EqualError(t, err, test.initializationErr)
				return
			}
			require.NoError(t, err, "Failed to initialize cpu scraper: %v", err)
			defer func() { assert.NoError(t, scraper.Close(context.Background())) }()

			metrics, err := scraper.ScrapeMetrics(context.Background())
			if test.expectedErr != "" {
				assert.EqualError(t, err, test.expectedErr)
				return
			}
			require.NoError(t, err, "Failed to scrape metrics: %v", err)

			assert.Equal(t, 1, metrics.Len())

			assertCPUMetricValid(t, metrics.At(0), cpuTimeDescriptor, test.expectedStartTime)

			if runtime.GOOS == "linux" {
				assertCPUMetricHasLinuxSpecificStateLabels(t, metrics.At(0))
			}

			internal.AssertSameTimeStampForAllMetrics(t, metrics)
		})
	}
}

func assertCPUMetricValid(t *testing.T, metric pdata.Metric, descriptor pdata.Metric, startTime pdata.TimestampUnixNano) {
	internal.AssertDescriptorEqual(t, descriptor, metric)
	if startTime != 0 {
		internal.AssertDoubleSumMetricStartTimeEquals(t, metric, startTime)
	}
	assert.GreaterOrEqual(t, metric.DoubleSum().DataPoints().Len(), 4*runtime.NumCPU())
	internal.AssertDoubleSumMetricLabelExists(t, metric, 0, cpuLabelName)
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 0, stateLabelName, userStateLabelValue)
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 1, stateLabelName, systemStateLabelValue)
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 2, stateLabelName, idleStateLabelValue)
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 3, stateLabelName, interruptStateLabelValue)
}

func assertCPUMetricHasLinuxSpecificStateLabels(t *testing.T, metric pdata.Metric) {
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 4, stateLabelName, niceStateLabelValue)
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 5, stateLabelName, softIRQStateLabelValue)
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 6, stateLabelName, stealStateLabelValue)
	internal.AssertDoubleSumMetricLabelHasValue(t, metric, 7, stateLabelName, waitStateLabelValue)
}
