package sunlightmeter

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ztkent/sunlight-meter/tsl2591"
)

//go:embed html/*
var templateFiles embed.FS

type SLMeter struct {
	*tsl2591.TSL2591
	LuxResultsChan chan LuxResults
	ResultsDB      *sql.DB
	cancel         context.CancelFunc
	Pid            int
}

type LuxResults struct {
	Lux          float64
	Infrared     float64
	Visible      float64
	FullSpectrum float64
	JobID        string
}

type Conditions struct {
	JobID                 string  `json:"jobID"`
	Lux                   float64 `json:"lux"`
	FullSpectrum          float64 `json:"fullSpectrum"`
	Visible               float64 `json:"visible"`
	Infrared              float64 `json:"infrared"`
	DateRange             string  `json:"dateRange"`
	RecordedHoursInRange  float64 `json:"recordedHoursInRange"`
	FullSunlightInRange   float64 `json:"fullSunlightInRange"`
	LightConditionInRange string  `json:"lightConditionInRange"`
	AverageLuxInRange     float64 `json:"averageLuxInRange"`
}

const (
	MAX_JOB_DURATION = 8 * time.Hour
	RECORD_INTERVAL  = 30 * time.Second
	DB_PATH          = "sunlightmeter.db"
)

// Start the sensor, and collect data in a loop
func (m *SLMeter) Start() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		log.Println("It's going to be a bright day!")
		if m.TSL2591 == nil {
			ServeResponse(w, r, "The sensor is not connected", http.StatusBadRequest)
			return
		} else if m.Enabled {
			ServeResponse(w, r, "The sensor is already started", http.StatusBadRequest)
			return
		}

		go func() {
			// Create a new context with a timeout to manage the sensor lifecycle
			ctx, cancel := context.WithTimeout(context.Background(), MAX_JOB_DURATION)
			m.cancel = cancel

			// Enable the sensor
			m.Enable()
			defer m.Disable()

			jobID := uuid.New().String()
			ticker := time.NewTicker(RECORD_INTERVAL)
			for {
				// Check if we've cancelled this job.
				select {
				case <-ctx.Done():
					log.Println("Job Cancelled, stopping sensor")
					return
				default:
				}

				// Read the sensor
				ch0, ch1, err := m.GetFullLuminosity()
				if err != nil {
					log.Println(fmt.Sprintf("The sensor failed to get luminosity: %s", err.Error()))
					m.LuxResultsChan <- LuxResults{
						JobID: jobID,
					}
					<-ticker.C
					continue
				}

				// Calculate the lux value from the sensor readings
				lux, err := m.CalculateLux(ch0, ch1)
				if err != nil {
					log.Println(fmt.Sprintf("The sensor failed to calculate lux: %s", err.Error()))
					log.Println("Attempting to set new optimal sensor gain")
					err = m.SetOptimalGain()
					if err != nil {
						log.Println(fmt.Sprintf("The sensor failed to determine new optimal gain: %s", err.Error()))
					} else {
						log.Println("The sensor has been reconfigured with a new optimal gain")
					}
					time.Sleep(5 * time.Second)
					continue
				}

				// Send the results to the LuxResultsChan
				m.LuxResultsChan <- LuxResults{
					Lux:          lux,
					Visible:      tsl2591.GetNormalizedOutput(tsl2591.TSL2591_VISIBLE, ch0, ch1),
					Infrared:     tsl2591.GetNormalizedOutput(tsl2591.TSL2591_INFRARED, ch0, ch1),
					FullSpectrum: tsl2591.GetNormalizedOutput(tsl2591.TSL2591_FULLSPECTRUM, ch0, ch1),
					JobID:        jobID,
				}
				<-ticker.C
			}
		}()
		w.WriteHeader(http.StatusOK)
		ServeResponse(w, r, "Sunlight Reading Started", http.StatusOK)
		return
	}
}

// Stop the sensor, and cancel the job context
func (m *SLMeter) Stop() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.TSL2591 == nil {
			ServeResponse(w, r, "The sensor is not connected", http.StatusBadRequest)
			return
		} else if !m.Enabled {
			ServeResponse(w, r, "The sensor is already stopped", http.StatusBadRequest)
			return
		}

		// Stop the sensor, cancel the job context
		defer m.Disable()
		m.cancel()

		w.WriteHeader(http.StatusOK)
		ServeResponse(w, r, "Sunlight Reading Stopped", http.StatusOK)
		return
	}
}

// Serve data about the most recent entry saved to the db
func (m *SLMeter) CurrentConditions() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if m.TSL2591 == nil {
			ServeResponse(w, r, "The sensor is not connected", http.StatusBadRequest)
			return
		} else if !m.Enabled {
			ServeResponse(w, r, "The sensor is not enabled", http.StatusBadRequest)
			return
		}
		conditions, err := m.getCurrentConditions()
		if err != nil {
			log.Println(err)
			ServeResponse(w, r, err.Error(), http.StatusInternalServerError)
			return
		}

		conditionsData, err := json.Marshal(conditions)
		if err != nil {
			log.Println(err)
			ServeResponse(w, r, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		ServeResponse(w, r, string(conditionsData), http.StatusOK)
		return
	}
}

// Return the most recent entry saved to the db
func (m *SLMeter) getCurrentConditions() (Conditions, error) {
	if m.TSL2591 == nil || !m.Enabled {
		return Conditions{}, nil
	}
	conditions := Conditions{}
	row := m.ResultsDB.QueryRow("SELECT job_id, lux, full_spectrum, visible, infrared FROM sunlight ORDER BY id DESC LIMIT 1")
	err := row.Scan(&conditions.JobID, &conditions.Lux, &conditions.FullSpectrum, &conditions.Visible, &conditions.Infrared)
	if err != nil {
		log.Println(err)
		return Conditions{}, err
	}
	return conditions, nil
}

// Check the signal strength of the wifi connection
func (m *SLMeter) SignalStrength() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cmd := exec.Command("sh", "-c", "iw dev wlan0 link | grep 'signal:' | awk '{print $2}'")
		output, err := cmd.Output()
		if err != nil {
			log.Println(err)
			ServeResponse(w, r, err.Error(), http.StatusInternalServerError)
			return
		}
		signalInt, err := strconv.Atoi(strings.TrimSpace(string(output)))
		if err != nil {
			ServeResponse(w, r, "Device is not connected to a network", http.StatusBadRequest)
			return
		}

		// Convert the signal to a strength value
		// https://git.openwrt.org/?p=project/iwinfo.git;a=blob;f=iwinfo_nl80211.c;hb=HEAD#l2885
		if signalInt < -110 {
			signalInt = -110
		} else if signalInt > -40 {
			signalInt = -40
		}

		// Scale the signal to a percentage
		strength := (signalInt + 110) * 100 / 70
		if strength < 0 {
			strength = 0
		} else if strength > 100 {
			strength = 100
		}

		log.Println("Signal: ", fmt.Sprintf("%d", signalInt), " dBm")
		log.Println("Strength: ", strength, "%")

		w.WriteHeader(http.StatusOK)
		ServeResponse(w, r, "Signal Strength: "+fmt.Sprintf("%d", signalInt)+" dBm\nQuality: "+fmt.Sprintf("%d", strength)+"%", http.StatusOK)
		return
	}
}

// Populate the response div with a message, or reply with a JSON message
func ServeResponse(w http.ResponseWriter, r *http.Request, message string, status int) {
	if strings.Contains(r.URL.Path, "/api/v1/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"message": message})
		return
	}

	tmpl, err := parseTemplateFile("html/response.gohtml")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, message)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func parseTemplateFile(path string) (*template.Template, error) {
	content, err := templateFiles.ReadFile(path)
	if err != nil {
		log.Fatalf("failed to read embedded template: %v", err)
	}

	tmpl, err := template.New("results").Parse(string(content))
	if err != nil {
		log.Fatalf("failed to parse template: %v", err)
	}
	return tmpl, nil
}

// Read from LuxResultsChan, write the results to sqlite
func (m *SLMeter) MonitorAndRecordResults() {
	log.Println("Monitoring for new Sunlight Messages...")
	for {
		select {
		case result := <-m.LuxResultsChan:
			log.Println(fmt.Sprintf("- JobID: %s, Lux: %.5f", result.JobID, result.Lux))
			if math.IsInf(result.Lux, 1) {
				log.Println("Lux is invalid, skipping record")
				continue
			}
			_, err := m.ResultsDB.Exec(
				"INSERT INTO sunlight (job_id, lux, full_spectrum, visible, infrared) VALUES (?, ?, ?, ?, ?)",
				result.JobID,
				fmt.Sprintf("%.5f", result.Lux),
				fmt.Sprintf("%.5e", result.FullSpectrum),
				fmt.Sprintf("%.5e", result.Visible),
				fmt.Sprintf("%.5e", result.Infrared),
			)
			if err != nil {
				log.Println(err)
			}
		}
	}
}
