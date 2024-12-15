package tools

import (
	"io"
	"log"
	"os"
)

type MultiWriter struct {
	Writers []io.Writer
}

// Record anything we log in the slm.log file
func init() {
	logFile, err := os.OpenFile("slm.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("error opening log file: %v", err)
	}
	multi := io.MultiWriter(logFile, os.Stdout)
	log.SetOutput(multi)
}

func (t *MultiWriter) Write(p []byte) (n int, err error) {
	for _, w := range t.Writers {
		n, err = w.Write(p)
		if err != nil {
			return
		}
	}
	return
}
