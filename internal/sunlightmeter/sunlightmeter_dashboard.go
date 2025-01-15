package sunlightmeter

import (
	"database/sql"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-echarts/go-echarts/v2/charts"
	"github.com/go-echarts/go-echarts/v2/components"
	"github.com/go-echarts/go-echarts/v2/opts"
	"github.com/go-echarts/go-echarts/v2/types"
)

// Serve the sqlite db for download
func (m *SLMeter) ServeResultsDB() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", "sunlightmeter.db"))
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, DB_PATH)
	}
}

// Serve the homepage
func (m *SLMeter) ServeDashboard() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fileContent, err := parseHTMLFile("html/dashboard.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(fileContent)
	}
}

func parseHTMLFile(path string) ([]byte, error) {
	content, err := templateFiles.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read embedded html file: %v", err)
	}
	return content, nil
}

// Serve the controls for the sensor, start/stop/export/current-conditions/signal-strength
func (m *SLMeter) ServeSunlightControls() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := parseTemplateFile("html/controls.gohtml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, nil)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// Status of the sensor
func (m *SLMeter) ServeSensorStatus() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tmpl, err := parseTemplateFile("html/status.gohtml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		type Status struct {
			Connected bool
			Enabled   bool
		}
		status := Status{}
		if m.TSL2591 == nil {
			status.Connected = false
		} else {
			status.Connected = true
			status.Enabled = m.Enabled
		}

		err = tmpl.Execute(w, status)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// Serve the results graph
func (m *SLMeter) ServeResultsGraph() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		startDate, endDate := parseStartAndEndDate(r)
		rows, err := m.ResultsDB.Query("SELECT lux, created_at FROM sunlight WHERE created_at BETWEEN ? AND ? ORDER BY created_at", startDate, endDate)
		if err != nil {
			log.Println(err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		var luxValues []opts.LineData
		var timeValues []string
		var maxLux int
		for rows.Next() {
			var lux string
			var createdAt time.Time
			if err := rows.Scan(&lux, &createdAt); err != nil {
				log.Println(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			luxFloat, err := strconv.ParseFloat(lux, 64)
			if err != nil {
				log.Println(err)
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			timeString := createdAt.Format("2006-01-02 15:04:05")
			if luxFloat > float64(maxLux) {
				maxLux = int(math.Ceil(luxFloat/5000) * 5000)
			}

			luxValues = append(luxValues, opts.LineData{Value: luxFloat})
			timeValues = append(timeValues, timeString)
		}

		line := charts.NewLine()
		levels := map[int]string{
			500:   "DarkGrey",
			1000:  "WhiteSmoke",
			10000: "SkyBlue",
			25000: "Yellow",
		}
		titles := map[int]string{
			500:   "Shade",
			1000:  "Partial Shade",
			10000: "Partial Sun",
			25000: "Full Sun",
		}

		for level, color := range levels {
			line.AddSeries(
				fmt.Sprintf("%s", titles[level]),
				func(level int, length int) []opts.LineData {
					data := make([]opts.LineData, length)
					for i := range data {
						data[i] = opts.LineData{Value: level}
					}
					return data
				}(level, len(timeValues)),
				charts.WithLineChartOpts(opts.LineChart{
					Color: color,
				}),
			)
		}

		line.SetGlobalOptions(
			charts.WithInitializationOpts(opts.Initialization{
				Theme: types.ThemeChalk,
			}),
			charts.WithTitleOpts(opts.Title{
				// Title: "Lux over time",
			}),
			charts.WithXAxisOpts(opts.XAxis{
				Name: "Time",
			}),
			charts.WithYAxisOpts(opts.YAxis{
				Name: "Lux",
				Min:  "0",
				Max:  fmt.Sprintf("%d", maxLux),
			}),
			charts.WithTooltipOpts(opts.Tooltip{
				Show:      true,
				Trigger:   "axis",
				TriggerOn: "mousemove",
				Formatter: "{a4}: {c4}<br> Time: {b0}",
			}),
			charts.WithToolboxOpts(opts.Toolbox{
				Show: true,
				Feature: &opts.ToolBoxFeature{
					SaveAsImage: &opts.ToolBoxFeatureSaveAsImage{
						Show:  true,
						Title: "Save as Image",
						Name:  "sunlight-meter",
					},
				},
			}),
		)
		line.SetXAxis(timeValues).AddSeries("Lux", luxValues)

		// Create a new page and add the line chart to it
		page := components.NewPage()
		page.AddCharts(line)
		w.Header().Set("Content-Type", "text/html")
		page.Render(w)

		// Trigger an update for the results tab
		w.Write([]byte(`<div id='resultUpdateTrigger' hx-post='/sunlightmeter/results' hx-target='#resultsContent' hx-trigger='load'></div>`))
		w.Write([]byte(`<script>document.title = "Sunlight Meter";</script>`))
	}
}

// Update the info in the results tab
func (m *SLMeter) ServeResultsTab() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conditions, err := m.getCurrentConditions()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		startDate, endDate := parseStartAndEndDate(r)
		conditions, err = m.getHistoricalConditions(conditions, startDate, endDate)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl, err := parseTemplateFile("html/results.gohtml")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		type ConditionsForDisplay struct {
			JobID                 string `json:"jobID"`
			Lux                   string `json:"lux"`
			FullSpectrum          string `json:"fullSpectrum"`
			Visible               string `json:"visible"`
			Infrared              string `json:"infrared"`
			DateRange             string `json:"dateRange"`
			RecordedHoursInRange  string `json:"recordedHoursInRange"`
			FullSunlightInRange   string `json:"fullSunlightInRange"`
			LightConditionInRange string `json:"lightConditionInRange"`
			AverageLuxInRange     string `json:"averageLuxInRange"`
			StartDate             string `json:"startDate"`
			EndDate               string `json:"endDate"`
		}
		err = tmpl.Execute(w, ConditionsForDisplay{
			JobID:                 conditions.JobID,
			Lux:                   fmt.Sprintf("%.4f", conditions.Lux),
			FullSpectrum:          fmt.Sprintf("%.4f", conditions.FullSpectrum),
			Visible:               fmt.Sprintf("%.4f", conditions.Visible),
			Infrared:              fmt.Sprintf("%.4f", conditions.Infrared),
			DateRange:             conditions.DateRange,
			RecordedHoursInRange:  fmt.Sprintf("%.4f", conditions.RecordedHoursInRange),
			FullSunlightInRange:   fmt.Sprintf("%.4f", conditions.FullSunlightInRange),
			LightConditionInRange: conditions.LightConditionInRange,
			AverageLuxInRange:     fmt.Sprintf("%.4f", conditions.AverageLuxInRange),
			StartDate:             startDate,
			EndDate:               endDate,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

// Return the most recent entry saved to the db
func (m *SLMeter) getHistoricalConditions(conditions Conditions, startDate string, endDate string) (Conditions, error) {
	if m.ResultsDB == nil {
		return conditions, nil
	}

	conditions.DateRange = fmt.Sprintf("%s - %s UTC", startDate, endDate)
	row := m.ResultsDB.QueryRow(`
    SELECT 
        COALESCE(AVG(lux), 0), 
        COALESCE(MIN(created_at), '0001-01-01 00:00:00'), 
        COALESCE(MAX(created_at), '0001-01-01 00:00:00') 
    FROM sunlight 
    WHERE created_at BETWEEN ? AND ?`, startDate, endDate)
	var oldest, mostRecent sql.NullString
	err := row.Scan(&conditions.AverageLuxInRange, &oldest, &mostRecent)
	if err != nil {
		return conditions, err
	}
	if conditions.AverageLuxInRange == 0 {
		conditions.LightConditionInRange = "No Data in Range"
		return conditions, nil
	}

	// Get the number of hours where the average lux was above 10k
	rows, err := m.ResultsDB.Query(`
    SELECT COUNT(*) 
    FROM (
        SELECT AVG(lux) as avg_lux 
        FROM sunlight 
        WHERE created_at BETWEEN ? AND ? 
        GROUP BY strftime('%H:%M', created_at)
    ) 
    WHERE avg_lux > 10000`, startDate, endDate)
	if err != nil {
		return conditions, err
	}

	defer rows.Close()
	var fullSunlightInRangeMin sql.NullFloat64
	if rows.Next() {
		err = rows.Scan(&fullSunlightInRangeMin)
		if err != nil {
			return conditions, err
		}
	}
	if fullSunlightInRangeMin.Valid {
		conditions.FullSunlightInRange = fullSunlightInRangeMin.Float64 / 60
	}

	// Determine the light condition for the date range
	if oldest.Valid && mostRecent.Valid {
		mostRecent, oldest, err := startAndEndDateToTime(oldest.String, mostRecent.String)
		if err != nil {
			return conditions, err
		}
		conditions.RecordedHoursInRange = oldest.Sub(mostRecent).Hours()
		if conditions.FullSunlightInRange/conditions.RecordedHoursInRange > 0.5 {
			conditions.LightConditionInRange = "Full Sun"
		} else if conditions.FullSunlightInRange/conditions.RecordedHoursInRange > 0.25 {
			conditions.LightConditionInRange = "Partial Sun"
		} else if conditions.FullSunlightInRange/conditions.RecordedHoursInRange > 0.1 {
			conditions.LightConditionInRange = "Partial Shade"
		} else {
			conditions.LightConditionInRange = "Shade"
		}
	}

	return conditions, nil
}

// Used to clear a div with htmx
func (m *SLMeter) Clear() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
	}
}

// Get the start and end dates from the request, format them for comparison with the DB
func parseStartAndEndDate(r *http.Request) (string, string) {
	r.ParseForm()
	startDate := r.FormValue("start")
	endDate := r.FormValue("end")
	layoutInput := "2006-01-02T15:04"
	layoutDB := "2006-01-02 15:04:05"
	if startDate == "" || endDate == "" {
		startDate = time.Now().UTC().Add(-8 * time.Hour).Format(layoutDB)
		endDate = time.Now().UTC().Format(layoutDB)
	} else {
		var err error
		var t time.Time

		// Assume they are in EST, who has users? Not me.
		loc, _ := time.LoadLocation("America/Indiana/Indianapolis")

		t, err = time.Parse(layoutInput, startDate)
		if err != nil {
			log.Println("Error parsing start date:", err)
		} else {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
			startDate = t.UTC().Format(layoutDB)
		}

		t, err = time.Parse(layoutInput, endDate)
		if err != nil {
			log.Println("Error parsing end date:", err)
		} else {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc)
			endDate = t.UTC().Format(layoutDB)
		}
	}
	return startDate, endDate
}

func startAndEndDateToTime(startDate string, endDate string) (time.Time, time.Time, error) {
	layoutDB := "2006-01-02 15:04:05"
	start, err := time.Parse(layoutDB, startDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	end, err := time.Parse(layoutDB, endDate)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, end, nil
}
