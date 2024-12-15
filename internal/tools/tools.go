package tools

import (
	"log"
	"net"
	"net/http"
	"time"
)

// Prevent out-of-network requests to dashboard endpoints
func CheckInNetwork(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			http.Error(w, "Invalid IP address", http.StatusBadRequest)
			return
		}
		if !isLocalAddress(parsedIP) {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}
func isLocalAddress(ip net.IP) bool {
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
	}
	for _, block := range privateBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr.Contains(ip) {
			return true
		}
	}
	return ip.String() == "127.0.0.1"
}

// Get the start and end dates from the request, format them for comparison with the DB
func ParseStartAndEndDate(r *http.Request) (string, string) {
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
func StartAndEndDateToTime(startDate string, endDate string) (time.Time, time.Time, error) {
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
