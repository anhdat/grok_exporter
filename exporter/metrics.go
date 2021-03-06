// Copyright 2016-2017 The grok_exporter Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package exporter

import (
	"fmt"
	"github.com/fstab/grok_exporter/config/v2"
	"github.com/fstab/grok_exporter/templates"
	"github.com/prometheus/client_golang/prometheus"
	"strconv"
)

type Metric interface {
	Name() string
	Collector() prometheus.Collector

	// Returns true if the line matched, and false if the line didn't match.
	Process(line string) (bool, error)
}

// Represents a Prometheus Counter
type incMetric struct {
	name      string
	regex     *OnigurumaRegexp
	labels    []templates.Template
	collector prometheus.Collector
	incFunc   func(m *OnigurumaMatchResult) error
}

// Represents a Prometheus Gauge, Histogram, or Summary
type observeMetric struct {
	name        string
	regex       *OnigurumaRegexp
	value       templates.Template
	labels      []templates.Template
	collector   prometheus.Collector
	observeFunc func(m *OnigurumaMatchResult, val float64) error
}

func NewCounterMetric(cfg *v2.MetricConfig, regex *OnigurumaRegexp) Metric {
	counterOpts := prometheus.CounterOpts{
		Name: cfg.Name,
		Help: cfg.Help,
	}
	if len(cfg.Labels) == 0 { // regular counter
		counter := prometheus.NewCounter(counterOpts)
		return &incMetric{
			name:      cfg.Name,
			regex:     regex,
			collector: counter,
			incFunc: func(_ *OnigurumaMatchResult) error {
				counter.Inc()
				return nil
			},
		}
	} else { // counterVec
		counterVec := prometheus.NewCounterVec(counterOpts, prometheusLabels(cfg.LabelTemplates))
		result := &incMetric{
			name:      cfg.Name,
			regex:     regex,
			labels:    cfg.LabelTemplates,
			collector: counterVec,
			incFunc: func(m *OnigurumaMatchResult) error {
				vals, err := labelValues(m, cfg.LabelTemplates)
				if err == nil {
					counterVec.WithLabelValues(vals...).Inc()
				}
				return err
			},
		}
		return result
	}
}

func NewGaugeMetric(cfg *v2.MetricConfig, regex *OnigurumaRegexp) Metric {
	gaugeOpts := prometheus.GaugeOpts{
		Name: cfg.Name,
		Help: cfg.Help,
	}
	if len(cfg.Labels) == 0 { // regular gauge
		gauge := prometheus.NewGauge(gaugeOpts)
		return &observeMetric{
			name:      cfg.Name,
			regex:     regex,
			value:     cfg.ValueTemplate,
			collector: gauge,
			observeFunc: func(_ *OnigurumaMatchResult, val float64) error {
				if cfg.Cumulative {
					gauge.Add(val)
				} else {
					gauge.Set(val)
				}
				return nil
			},
		}
	} else { // gaugeVec
		gaugeVec := prometheus.NewGaugeVec(gaugeOpts, prometheusLabels(cfg.LabelTemplates))
		return &observeMetric{
			name:      cfg.Name,
			regex:     regex,
			value:     cfg.ValueTemplate,
			collector: gaugeVec,
			labels:    cfg.LabelTemplates,
			observeFunc: func(m *OnigurumaMatchResult, val float64) error {
				vals, err := labelValues(m, cfg.LabelTemplates)
				if err == nil {
					if cfg.Cumulative {
						gaugeVec.WithLabelValues(vals...).Add(val)
					} else {
						gaugeVec.WithLabelValues(vals...).Set(val)
					}
				}
				return err
			},
		}
	}
}

func NewHistogramMetric(cfg *v2.MetricConfig, regex *OnigurumaRegexp) Metric {
	histogramOpts := prometheus.HistogramOpts{
		Name: cfg.Name,
		Help: cfg.Help,
	}
	if len(cfg.Buckets) > 0 {
		histogramOpts.Buckets = cfg.Buckets
	}
	if len(cfg.Labels) == 0 { // regular histogram
		histogram := prometheus.NewHistogram(histogramOpts)
		return &observeMetric{
			name:      cfg.Name,
			regex:     regex,
			value:     cfg.ValueTemplate,
			collector: histogram,
			observeFunc: func(_ *OnigurumaMatchResult, val float64) error {
				histogram.Observe(val)
				return nil
			},
		}
	} else { // histogramVec
		histogramVec := prometheus.NewHistogramVec(histogramOpts, prometheusLabels(cfg.LabelTemplates))
		return &observeMetric{
			name:      cfg.Name,
			regex:     regex,
			value:     cfg.ValueTemplate,
			collector: histogramVec,
			labels:    cfg.LabelTemplates,
			observeFunc: func(m *OnigurumaMatchResult, val float64) error {
				vals, err := labelValues(m, cfg.LabelTemplates)
				if err == nil {
					histogramVec.WithLabelValues(vals...).Observe(val)
				}
				return err
			},
		}
	}
}

func NewSummaryMetric(cfg *v2.MetricConfig, regex *OnigurumaRegexp) Metric {
	summaryOpts := prometheus.SummaryOpts{
		Name: cfg.Name,
		Help: cfg.Help,
	}
	if len(cfg.Quantiles) > 0 {
		summaryOpts.Objectives = cfg.Quantiles
	}
	if len(cfg.Labels) == 0 { // regular summary
		summary := prometheus.NewSummary(summaryOpts)
		return &observeMetric{
			name:      cfg.Name,
			regex:     regex,
			value:     cfg.ValueTemplate,
			collector: summary,
			observeFunc: func(_ *OnigurumaMatchResult, val float64) error {
				summary.Observe(val)
				return nil
			},
		}
	} else { // summaryVec
		summaryVec := prometheus.NewSummaryVec(summaryOpts, prometheusLabels(cfg.LabelTemplates))
		return &observeMetric{
			name:      cfg.Name,
			regex:     regex,
			value:     cfg.ValueTemplate,
			collector: summaryVec,
			labels:    cfg.LabelTemplates,
			observeFunc: func(m *OnigurumaMatchResult, val float64) error {
				vals, err := labelValues(m, cfg.LabelTemplates)
				if err == nil {
					summaryVec.WithLabelValues(vals...).Observe(val)
				}
				return err
			},
		}
	}
}

// Return: true if the line matched, false if it didn't match.
func (m *incMetric) Process(line string) (bool, error) {
	matchResult, err := m.regex.Match(line)
	if err != nil {
		return false, fmt.Errorf("error while processing metric %v: %v", m.name, err.Error())
	}
	defer matchResult.Free()
	if matchResult.IsMatch() {
		err = m.incFunc(matchResult)
		return true, err
	} else {
		return false, nil
	}
}

// Return: true if the line matched, false if it didn't match.
func (m *observeMetric) Process(line string) (bool, error) {
	matchResult, err := m.regex.Match(line)
	if err != nil {
		return false, fmt.Errorf("error while processing metric %v: %v", m.name, err.Error())
	}
	defer matchResult.Free()
	if matchResult.IsMatch() {
		stringVal, err := evalTemplate(matchResult, m.value)
		if err != nil {
			return true, fmt.Errorf("error while processing metric %v: %v", m.name, err.Error())
		}
		floatVal, err := strconv.ParseFloat(stringVal, 64)
		if err != nil {
			return true, fmt.Errorf("error while processing metric %v: value '%v' matches '%v', which is not a valid number.", m.name, m.value, stringVal)
		}
		err = m.observeFunc(matchResult, floatVal)
		return true, err
	} else {
		return false, nil
	}
}

func (m *incMetric) Name() string {
	return m.name
}

func (m *observeMetric) Name() string {
	return m.name
}

func (m *incMetric) Collector() prometheus.Collector {
	return m.collector
}

func (m *observeMetric) Collector() prometheus.Collector {
	return m.collector
}

func labelValues(matchResult *OnigurumaMatchResult, templates []templates.Template) ([]string, error) {
	result := make([]string, 0, len(templates))
	for _, t := range templates {
		value, err := evalTemplate(matchResult, t)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func evalTemplate(matchResult *OnigurumaMatchResult, t templates.Template) (string, error) {
	grokValues := make(map[string]string, len(t.ReferencedGrokFields()))
	for _, field := range t.ReferencedGrokFields() {
		value, err := matchResult.Get(field)
		if err != nil {
			return "", err
		}
		grokValues[field] = value
	}
	return t.Execute(grokValues)
}

func prometheusLabels(templates []templates.Template) []string {
	promLabels := make([]string, 0, len(templates))
	for _, t := range templates {
		promLabels = append(promLabels, t.Name())
	}
	return promLabels
}
