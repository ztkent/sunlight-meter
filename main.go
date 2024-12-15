package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/ztkent/sunlight-meter/internal/sunlightmeter"
	slm "github.com/ztkent/sunlight-meter/internal/sunlightmeter"
	"github.com/ztkent/sunlight-meter/internal/tools"
	"github.com/ztkent/sunlight-meter/tsl2591"
)

/*
	This is going to be the primary entry point for the Sunlight Meter application.
	It should be running at startup, on a Raspberry Pi, with the TSL2591 sensor connected.
*/

func main() {
	pid := os.Getpid()
	log.Println("SunlightMeter [" + fmt.Sprintf("%d", pid) + "]")

	// connect to the lux sensor
	device, err := tsl2591.NewTSL2591(
		tsl2591.TSL2591_GAIN_LOW,
		tsl2591.TSL2591_INTEGRATIONTIME_300MS,
		"/dev/i2c-1",
	)
	if err != nil {
		log.Fatalf("Failed to connect to the TSL2591 sensor: %v", err)
	}

	// connect to the sqlite database
	slmDB, err := tools.ConnectSqlite(slm.DB_PATH)
	if err != nil {
		// Unlike connecting to the sensor, this should always work.
		log.Fatalf("Failed to connect to the sqlite database: %v", err)
	}

	// Initialize router
	r := chi.NewRouter()
	// Log requests and recover from panics
	r.Use(middleware.Logger)
	r.Use(handleServerPanic)

	// Define routes
	defineRoutes(r, &slm.SLMeter{
		TSL2591:        device,
		ResultsDB:      slmDB,
		LuxResultsChan: make(chan slm.LuxResults),
		Pid:            pid,
	})

	if os.Getenv("SSL") == "true" {
		// Generate a self-signed certificate if one doesn't exist
		tools.EnsureCertificate("cert.pem", "key.pem")

		// Start server
		app_port := "443"
		certPath := "cert.pem"
		keyPath := "key.pem"

		log.Printf("Starting HTTPS server on port %s", app_port)
		err = http.ListenAndServeTLS(":"+app_port, certPath, keyPath, r)
		if err != nil {
			log.Fatalf("Failed to start HTTPS server: %v", err)
		}
	} else {
		// Start server
		app_port := "80"
		log.Printf("Starting HTTP server on port %s", app_port)
		err = http.ListenAndServe(":"+app_port, r)
		if err != nil {
			log.Fatalf("Failed to start HTTP server: %v", err)
		}
	}

	return
}

func defineRoutes(r *chi.Mux, meter *slm.SLMeter) {
	// Listen for any result messages from our jobs, record them in sqlite
	go meter.MonitorAndRecordResults()

	// Sunlight Meter Dashboard Controls
	r.Get("/", meter.ServeDashboard())
	r.Route("/sunlightmeter", func(r chi.Router) {
		r.Get("/start", meter.Start())
		r.Get("/stop", meter.Stop())
		r.Get("/signal-strength", meter.SignalStrength())
		r.Get("/current-conditions", meter.CurrentConditions())
		r.Get("/export", meter.ServeResultsDB())
		r.Post("/graph", meter.ServeResultsGraph())
		r.Get("/controls", meter.ServeSunlightControls())
		r.Get("/status", meter.ServeSensorStatus())
		r.Post("/results", meter.ServeResultsTab())
		r.Get("/clear", meter.Clear())
	})

	// Sunlight Meter API, these serve a JSON response
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/start", meter.Start())
		r.Get("/stop", meter.Stop())
		r.Get("/signal-strength", meter.SignalStrength())
		r.Get("/current-conditions", meter.CurrentConditions())
		r.Get("/export", meter.ServeResultsDB())
	})

	// Route for service identification
	r.Get("/id", func(w http.ResponseWriter, r *http.Request) {
		response := struct {
			ServiceName string `json:"service_name"`
		}{
			ServiceName: "Sunlight Meter",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	})

	// Serve static files
	workDir, _ := os.Getwd()
	filesDir := filepath.Join(workDir, "internal", "sunlightmeter")
	FileServer(r, "/", http.Dir(filesDir))
}

func FileServer(r chi.Router, path string, root http.FileSystem) {
	r.Get(path+"*", func(w http.ResponseWriter, r *http.Request) {
		http.StripPrefix(path, http.FileServer(root)).ServeHTTP(w, r)
	})
}

func handleServerPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				sunlightmeter.ServeResponse(w, r, (fmt.Sprintf("%v", err)), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
