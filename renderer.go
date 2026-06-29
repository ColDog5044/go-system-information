package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type reportRenderer interface {
	Start(title string, sampleInterval time.Duration)
	Section(title string)
	Group(title string)
	Field(indent int, label, value string)
	List(label string, values []string, limit int)
	Finish() error
}

var currentRenderer reportRenderer = newConsoleRenderer(io.Discard)

func setReportRenderer(renderer reportRenderer) {
	currentRenderer = renderer
}

func startReport(title string, sampleInterval time.Duration) {
	currentRenderer.Start(title, sampleInterval)
}

func finishReport() error {
	return currentRenderer.Finish()
}

type consoleRenderer struct {
	writer io.Writer
}

func newConsoleRenderer(writer io.Writer) *consoleRenderer {
	return &consoleRenderer{writer: writer}
}

func (r *consoleRenderer) Start(title string, sampleInterval time.Duration) {
	fmt.Fprintln(r.writer, title)
	fmt.Fprintln(r.writer, strings.Repeat("=", len(title)))
	fmt.Fprintf(r.writer, "Measuring rate-based counters for %s...\n", sampleInterval)
}

func (r *consoleRenderer) Section(title string) {
	fmt.Fprintf(r.writer, "\n%s\n%s\n", title, strings.Repeat("-", len(title)))
}

func (r *consoleRenderer) Group(title string) {
	fmt.Fprintf(r.writer, "\n  %s\n", title)
}

func (r *consoleRenderer) Field(indent int, label, value string) {
	fmt.Fprintf(r.writer, "%s%-24s %s\n", strings.Repeat(" ", indent), label+":", value)
}

func (r *consoleRenderer) List(label string, values []string, limit int) {
	fmt.Fprintf(r.writer, "  %s:\n", label)
	for i, value := range values {
		if limit > 0 && i >= limit {
			fmt.Fprintf(r.writer, "    ... %d more\n", len(values)-limit)
			return
		}
		fmt.Fprintf(r.writer, "    %s\n", value)
	}
}

func (r *consoleRenderer) Finish() error {
	return nil
}

type jsonRenderer struct {
	writer         io.Writer
	report         jsonReport
	currentGroup   *jsonGroup
	currentSection *jsonSection
}

type jsonReport struct {
	Title                     string        `json:"title"`
	GeneratedAt               string        `json:"generated_at"`
	RateSampleIntervalSeconds float64       `json:"rate_sample_interval_seconds"`
	Sections                  []jsonSection `json:"sections"`
}

type jsonSection struct {
	Name   string      `json:"name"`
	Fields []jsonField `json:"fields,omitempty"`
	Groups []jsonGroup `json:"groups,omitempty"`
	Lists  []jsonList  `json:"lists,omitempty"`
}

type jsonGroup struct {
	Name   string      `json:"name"`
	Fields []jsonField `json:"fields,omitempty"`
	Lists  []jsonList  `json:"lists,omitempty"`
}

type jsonField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type jsonList struct {
	Label        string   `json:"label"`
	Values       []string `json:"values"`
	Displayed    int      `json:"displayed"`
	Total        int      `json:"total"`
	OmittedCount int      `json:"omitted_count"`
}

func newJSONRenderer(writer io.Writer) *jsonRenderer {
	return &jsonRenderer{writer: writer}
}

func (r *jsonRenderer) Start(title string, sampleInterval time.Duration) {
	r.report = jsonReport{
		Title:                     title,
		GeneratedAt:               time.Now().Format(time.RFC3339),
		RateSampleIntervalSeconds: sampleInterval.Seconds(),
	}
}

func (r *jsonRenderer) Section(title string) {
	r.report.Sections = append(r.report.Sections, jsonSection{Name: title})
	r.currentSection = &r.report.Sections[len(r.report.Sections)-1]
	r.currentGroup = nil
}

func (r *jsonRenderer) Group(title string) {
	if r.currentSection == nil {
		r.Section("Ungrouped")
	}
	r.currentSection.Groups = append(r.currentSection.Groups, jsonGroup{Name: title})
	r.currentGroup = &r.currentSection.Groups[len(r.currentSection.Groups)-1]
}

func (r *jsonRenderer) Field(_ int, label, value string) {
	field := jsonField{Label: label, Value: value}
	if r.currentGroup != nil {
		r.currentGroup.Fields = append(r.currentGroup.Fields, field)
		return
	}
	if r.currentSection == nil {
		r.Section("Ungrouped")
	}
	r.currentSection.Fields = append(r.currentSection.Fields, field)
}

func (r *jsonRenderer) List(label string, values []string, limit int) {
	displayed := values
	omitted := 0
	if limit > 0 && len(values) > limit {
		displayed = values[:limit]
		omitted = len(values) - limit
	}
	list := jsonList{
		Label:        label,
		Values:       displayed,
		Displayed:    len(displayed),
		Total:        len(values),
		OmittedCount: omitted,
	}
	if r.currentGroup != nil {
		r.currentGroup.Lists = append(r.currentGroup.Lists, list)
		return
	}
	if r.currentSection == nil {
		r.Section("Ungrouped")
	}
	r.currentSection.Lists = append(r.currentSection.Lists, list)
}

func (r *jsonRenderer) Finish() error {
	encoder := json.NewEncoder(r.writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(r.report)
}
