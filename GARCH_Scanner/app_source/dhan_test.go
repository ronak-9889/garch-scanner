package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// Dhan answers DH-907 for a toDate in the future (as reported from a real run). Daily must still
// return the data by falling back, and a window with no data must not be fatal.
func TestDailyFallsBackOnDH907(t *testing.T) {
	today := time.Now().In(IST).Format("2006-01-02")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		from, to := req["fromDate"].(string), req["toDate"].(string)
		if to > today || from < "2020-10-01" { // future date, or before the data exists
			w.WriteHeader(400)
			w.Write([]byte(`{"errorType":"Data_API_Error","errorCode":"DH-907","errorMessage":"System is unable to fetch data due to incorrect parameters or no data present"}`))
			return
		}
		out := map[string][]float64{}
		for d := time.Now().In(IST).AddDate(0, 0, -400); d.Before(time.Now().In(IST).AddDate(0, 0, -1)); d = d.AddDate(0, 0, 1) {
			if ds := d.Format("2006-01-02"); ds >= from && ds < to {
				ts := float64(time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, IST).Unix())
				out["timestamp"] = append(out["timestamp"], ts)
				for _, k := range []string{"open", "high", "low", "close", "volume"} {
					out[k] = append(out[k], 100)
				}
			}
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	dhanBase = srv.URL
	d := NewDhan("1", "tok")
	cs, err := d.Daily("13", "IDX_I", "INDEX", time.Now().In(IST).AddDate(-6, 0, 0), time.Now().In(IST).AddDate(0, 0, 1))
	if err != nil || len(cs) < 300 {
		t.Fatalf("expected data despite DH-907 on future/early dates, got %d candles, err %v", len(cs), err)
	}
}
