package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/common-nighthawk/go-figure"
	ui "github.com/visago/termui/v3"
	"github.com/visago/termui/v3/widgets"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/go-co-op/gocron"
)

type Datasource struct {
	Title string  `json:"title"`
	Query string  `json:"Query"`
	Prom  string  `json:"prom"`
	Unit  string  `json:"unit"`
	Warn  float64 `json:"warn"`
	Error float64 `json:"error"`
}

var fontClock string
var fontClockWidth int // For right align
var fontDate string
var fontDateWidth int // For right align
var fontLabel string

var dsConfig []Datasource
var datasourceCount = 0
var flagTz string
var flagProm string
var flagFile string
var flagTest bool
var flagRefresh int
var timezone *time.Location
var width int
var height int

const nullValue = -999 // Since float64 cannot be null, we just use a unique value

func loadConfig() {
	flag.BoolVar(&flagTest, "test", false, "test")
	flag.IntVar(&flagRefresh, "refresh", 15, "Refresh rate (for metrics)")
	flag.StringVar(&flagTz, "timezone", "Asia/Singapore", "Timezone")
	flag.StringVar(&flagFile, "file", "dashclock.json", "Prometheus sources in JSON format")
	flag.Parse()

	if _, err := os.Stat(flagFile); err == nil { // If file exists
		jsonFile, err := os.Open(flagFile)
		if err != nil {
			log.Fatal(err)
		}
		byteValue, _ := io.ReadAll(jsonFile)
		err = json.Unmarshal(byteValue, &dsConfig)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		log.Fatalf("Missing json config %s", flagFile)
	}

	datasourceCount = len(dsConfig)
	if datasourceCount == 0 {
		log.Fatalf("No datasources configured")
	}
	var err error
	timezone, err = time.LoadLocation(flagTz)
	if err != nil {
		log.Fatalf("failed to load timezone 1: %v", err)
	}
}

func main() {
	var clockColor = "yellow"
	var refreshUi = true  // When set this will cause a UI refresh/clear
	var syncPromCount = 0 // This provides a counter to use for looping through all the datasource polls
	loadConfig()          // Loads flags and configs

	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close() // Note that log.Fatal breaks this :(

	cron := gocron.NewScheduler(time.UTC) // Timezone doesn't matter as we only do by seconds

	cron.SingletonModeAll()
	cron.SetMaxConcurrentJobs(1, gocron.WaitMode)

	pClock := widgets.NewParagraph()
	pClock.Border = false
	pDate := widgets.NewParagraph()
	pDate.Border = false
	pMetric := widgets.NewParagraph()
	pMetric.Border = false
	chart0 := widgets.NewPlot()
	chart0.Border = false

	resizeUi := func() { // This is to resize the window. Executed on start and resize
		width, height = ui.TerminalDimensions()
		if width == 240 && height == 30 { // This for the 8" LCD
			pClock.SetRect(100, -3, width-28, 17)
			pDate.SetRect(width-30, -1, width+1, 17)
			chart0.SetRect(0, 16, width+1, height+1)
			pMetric.SetRect(0, 4, width-140, 17)
			fontClockWidth = 109
			fontDateWidth = 29
			fontClock = "doh"
			fontDate = "standard"
			fontLabel = "standard"
			ui.Clear()
		} else if width >= 131 && height >= 30 {
			pClock.SetRect(0, -3, width-28, 17)
			pDate.SetRect(width-30, -1, width+1, 17)
			chart0.SetRect(0, 17, width+1, height+1)
			pMetric.SetRect(0, 4, width-140, 17)
			fontClockWidth = width - 28 - 4
			fontDateWidth = 29
			fontClock = "doh"
			fontDate = "standard"
			fontLabel = ""
			ui.Clear()
		} else if width >= 68 && height >= 20 {
			pClock.SetRect(0, 0, width-16, 11)
			pDate.SetRect(width-18, -1, width+1, 11)
			chart0.SetRect(0, 11, width+1, height+1)
			pMetric.SetRect(0, 4, width-140, 11)
			fontClockWidth = width - 16 - 4
			fontDateWidth = 17
			fontClock = "colossal"
			fontDate = "mini"
			fontLabel = ""
			ui.Clear()
		} else if width >= 40 && height >= 16 {
			pClock.SetRect(-2, -1, width-4, 6)
			pDate.SetRect(width-5, 1, width+1, 6)
			chart0.SetRect(0, 6, width+1, height+1)
			pMetric.SetRect(0, 4, width-140, 6)
			fontClockWidth = width - 6 - 2
			fontDateWidth = 4
			fontClock = "standard"
			fontDate = "term"
			fontLabel = ""
			ui.Clear()
		} else {
			pClock.SetRect(-1, -1, width+1, height+1)
			pClock.Text = fmt.Sprintf("[Unsupported terminal size of %0d x %0d\nResize or (q)uit](fg:red)", width, height)
			fontClock = ""
			ui.Clear()
			ui.Render(pClock)
		}
	}

	syncProm := func() { // This loads data from prometheus and draws the graph
		fontColor := "white"                 // Default color
		i := syncPromCount % datasourceCount // Index for the datasource config
		r0, l0 := prometheusQueryRange(dsConfig[i].Prom, dsConfig[i].Query, width-8, 60, nullValue, timezone)
		if len(r0) > 0 {
			chart0.Data = [][]float64{r0}
			chart0.DataLabels = l0
			chart0.LabelAxesX = true
			chart0.LineColors[0] = ui.ColorGreen
			if dsConfig[i].Warn > 0 { // If not defined, we just assume no warning/error colors
				for _, d := range r0 {
					if d > dsConfig[i].Warn && chart0.LineColors[0] == ui.ColorGreen {
						chart0.LineColors[0] = ui.ColorYellow
					} else if d == nullValue {
						chart0.LineColors[0] = ui.ColorMagenta
					} else if d > dsConfig[i].Error {
						chart0.LineColors[0] = ui.ColorRed
					}
				}
				if r0[len(r0)-1] > dsConfig[i].Error {
					fontColor = "red"
				} else if r0[len(r0)-1] < 0 {
					fontColor = "red"
				} else if r0[len(r0)-1] > dsConfig[i].Warn {
					fontColor = "yellow"
				}
			}
			if len(fontLabel) > 0 {
				myFigLabel := figure.NewFigure(dsConfig[i].Title, fontLabel, false)
				myFigValue := figure.NewFigure(fmt.Sprintf("%01.0f%s", r0[len(r0)-1], dsConfig[i].Unit), fontLabel, false)
				pMetric.Text = fmt.Sprintf("[%s](fg:white)\n[%s](fg:%s)", strings.Join(myFigLabel.Slicify(), "\n"), strings.Join(myFigValue.Slicify(), "\n"), fontColor)
				ui.Render(pMetric)
			} else {
				chart0.Title = fmt.Sprintf("%s %01.0f%s", dsConfig[i].Title, r0[len(r0)-1], dsConfig[i].Unit)
			}
			ui.Render(chart0)
		} else {
			if len(fontLabel) > 0 {
				fontColor = "red"
				myFigLabel := figure.NewFigure(dsConfig[i].Title, fontLabel, false)
				myFigValue := figure.NewFigure("PROM ERROR", fontLabel, false)
				pMetric.Text = fmt.Sprintf("[%s](fg:red)\n[%s](fg:%s)", strings.Join(myFigLabel.Slicify(), "\n"), strings.Join(myFigValue.Slicify(), "\n"), fontColor)
				ui.Render(pMetric)
			} else {
				chart0.Title = fmt.Sprintf("%s PROM ERROR %s", dsConfig[i].Title, dsConfig[i].Prom)
			}
			ui.Render(chart0)
		}
		syncPromCount++

	}

	syncClock := func() { // This draws the clock
		timeNow := time.Now().In(timezone).Format("1504")
		dateNow := time.Now().In(timezone).Format("01")
		monthNow := time.Now().In(timezone).Format("Jan")
		dayNow := time.Now().In(timezone).Format("Mon")

		if flagTest { // Mainly for font alignment work
			timeNow = "2359"
			dateNow = "28"
			monthNow = "Mar"
			dayNow = "Wed"
		}

		clockFormat := "%c%c:%c%c"
		if fontClock == "doh" { // doh font is very close, we space it out
			clockFormat = "%c  %c  :  %c  %c"
		}
		myFigClock := figure.NewFigure(fmt.Sprintf(clockFormat, timeNow[0], timeNow[1], timeNow[2], timeNow[3]), fontClock, false)
		pClock.Text = fmt.Sprintf("[%s](fg:%s)", strings.Join(rightAlignText(myFigClock.Slicify(), fontClockWidth), "\n"), clockColor)

		myFigDate := figure.NewFigure(dateNow, fontDate, false)
		myFigDay := figure.NewFigure(dayNow, fontDate, false)
		myFigMonth := figure.NewFigure(monthNow, fontDate, false)
		pDate.Text = fmt.Sprintf("[%s](fg:white)\n[%s](fg:white)\n[%s](fg:white)",
			strings.Join(rightAlignText(myFigDay.Slicify(), fontDateWidth), "\n"),
			strings.Join(rightAlignText(myFigDate.Slicify(), fontDateWidth), "\n"),
			strings.Join(rightAlignText(myFigMonth.Slicify(), fontDateWidth), "\n")) // We right align this so the date is not so close to the clock
		ui.Render(pClock, pDate)
	}

	syncTerminal := func() {
		if refreshUi {
			resizeUi()
			refreshUi = false
		}
		if len(fontClock) > 0 { // 0 means display not good enough
			syncClock()
			syncProm()
		}
	}

	markRefresh := func() {
		refreshUi = true
	}

	cron.Every(1).Hour().SingletonMode().Tag("hourly").Do(markRefresh)
	cron.Every(flagRefresh).Second().SingletonMode().Tag("sync").Do(syncTerminal)
	cron.StartAsync()

	uiEvents := ui.PollEvents()
	for {
		select {
		case e := <-uiEvents:
			switch e.ID {
			case "q", "<C-c>":
				return
			case "`", "1", "2", "3", "4", "5", "6", "7", "8", "9", "0": // Quick jumps, yes `=0 as the first source
				i, err := strconv.Atoi(e.ID)
				if err != nil {
					i = 0
				}
				syncPromCount = i
				cron.RunByTag("sync")
			case "<Left>", "<Down>": // Go previous
				syncPromCount += datasourceCount - 2
				cron.RunByTag("sync")
			case "<Right>", "<Up>": // Go next
				cron.RunByTag("sync")
			case "<Space>": // Refresh current
				syncPromCount += datasourceCount - 1
				refreshUi = true
				cron.RunByTag("sync")
			case "<Resize>":
				// Minimal support for resizing, note that going to smaller sizes causes an error
				// Spamming resize will cause a crash as it attempts to redraw while screen size changes
				syncPromCount += datasourceCount - 1
				refreshUi = true
				cron.RunByTag("sync")
			}
		}
	}

}

func rightAlignText(text []string, width int) []string { // Right aligns a string array using width as size of box to pad
	longestLength := 0
	for _, l := range text {
		if len(l) > longestLength {
			longestLength = len(l)
		}
	}
	if longestLength < width {
		for i, l := range text {
			text[i] = strings.Repeat(" ", width-longestLength) + l
		}
	}
	return text
}

func prometheusQueryRange(promFlag string, query string, length int, intervalSeconds int, nilValue float64, tz *time.Location) ([]float64, []string) {
	var values []float64
	var labels []string
	client, err := api.NewClient(api.Config{
		Address: promFlag,
	})
	if err != nil {
		ui.Close()
		log.Printf("Error creating client: %v\n", err)
		os.Exit(1)
	}

	v1api := v1.NewAPI(client)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	timeNow := time.Now().Round(time.Second) // We want a nice round number

	r := v1.Range{
		Start: timeNow.Add(-time.Duration(length*intervalSeconds) * time.Second),
		End:   timeNow,
		Step:  time.Duration(intervalSeconds) * time.Second, // Data will be per minute
	}

	result, _, err := v1api.QueryRange(ctx, query, r, v1.WithTimeout(5*time.Second))
	if err != nil {
		return values, labels
	}
	switch result.Type() {
	case model.ValMatrix:
	default:
		log.Fatalf("Only support model.Matrix return, got %#v", result)
	}
	for i := length; i >= 0; i-- { // Flipped since we cant drawRight
		timeThen := timeNow.Add(-time.Second * time.Duration(i*intervalSeconds))
		values = append(values, findPromValue(*result.(model.Matrix)[0], timeThen, nilValue))
		labels = append(labels, timeThen.In(tz).Format("15:04"))
	}
	return values, labels
}

func findPromValue(sampleSet model.SampleStream, timestamp time.Time, nilValue float64) float64 { // This avoids having to do a lookup table
	for _, sampleValue := range sampleSet.Values {
		if sampleValue.Timestamp.Time() == timestamp {
			return float64(sampleValue.Value)
		}
	}
	return nilValue
}
