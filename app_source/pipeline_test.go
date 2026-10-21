package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A fake Dhan, fed from the real Nifty CSVs in the pack, to run the whole pipeline end to end.
func TestPipelineEndToEnd(t *testing.T) {
	code := "../code/"
	read := func(p string) [][]string {
		f, err := os.Open(code + p)
		if err != nil {
			t.Skip("pack data not found")
		}
		defer f.Close()
		rows, _ := csv.NewReader(f).ReadAll()
		return rows[1:]
	}
	daily, intra := read("nifty50_daily.csv"), read("nifty_5min.csv")
	rng := rand.New(rand.NewSource(1))
	ts := func(date string, minOfDay int) float64 {
		d, _ := time.ParseInLocation("2006-01-02", date, IST)
		return float64(d.Add(time.Duration(minOfDay) * time.Minute).Unix())
	}
	fv := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("access-token") != "good" {
			w.WriteHeader(401)
			w.Write([]byte(`{"errorType":"Invalid_Authentication","errorCode":"DH-901","errorMessage":"bad"}`))
			return
		}
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		out := map[string][]float64{}
		add := func(tm, o, h, l, c float64) {
			out["timestamp"] = append(out["timestamp"], tm)
			out["open"] = append(out["open"], o)
			out["high"] = append(out["high"], h)
			out["low"] = append(out["low"], l)
			out["close"] = append(out["close"], c)
			out["volume"] = append(out["volume"], 1)
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/charts/intraday"):
			from, to := req["fromDate"].(string)[:10], req["toDate"].(string)[:10]
			for _, row := range intra {
				if row[0] >= from && row[0] < to {
					add(ts(row[0], 9*60+15+int(fv(row[1]))), fv(row[2]), fv(row[3]), fv(row[4]), fv(row[5]))
				}
			}
		case req["securityId"] == "13":
			from, to := req["fromDate"].(string), req["toDate"].(string)
			for _, row := range daily {
				if row[0] >= from && row[0] < to {
					add(ts(row[0], 0), fv(row[1]), fv(row[2]), fv(row[3]), fv(row[4]))
				}
			}
		default: // a stock: geometric random walk on the same dates
			from, to := req["fromDate"].(string), req["toDate"].(string)
			p := 100.0 + float64(len(req["securityId"].(string)))
			for _, row := range daily {
				p *= math.Exp(0.012 * rng.NormFloat64())
				if row[0] >= from && row[0] < to {
					add(ts(row[0], 0), p, p, p, p)
				}
			}
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	dhanBase = srv.URL

	dh := NewDhan("1", "bad")
	if _, err := dh.Daily("13", "IDX_I", "INDEX", time.Date(2020, 1, 1, 0, 0, 0, 0, IST), time.Now()); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("bad token should be reported clearly, got %v", err)
	}

	dh = NewDhan("1", "good")
	now := time.Date(2026, 9, 23, 20, 0, 0, 0, IST)
	d, err := dh.Daily("13", "IDX_I", "INDEX", now.AddDate(-6, 0, 0), now.AddDate(0, 0, 1))
	if err != nil {
		t.Fatal(err)
	}
	in, err := dh.Intraday5("13", "IDX_I", "INDEX", now.AddDate(0, 0, -365), now.AddDate(0, 0, 1), nil)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("daily", len(d), "intraday bars", len(in))
	rd, err := BuildReplay(d, in, now)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("totals %+v check %+v\nparams %+v\ntomorrow %+v\n", rd.Totals, rd.Check, rd.Params, rd.Tomorrow)
	for _, h := range rd.Horizons {
		fmt.Printf("horizon %s sigma %.3f wide +-%.2f%% %.0f-%.0f check %+v\n", h.Label, h.Sigma, h.Wide, h.Lo2, h.Hi2, h.Check)
	}
	fmt.Println("days", len(rd.Days), "intraday_from", rd.IntradayFrom, "breaks", len(rd.Breaks), "biggest", rd.Biggest, "named", rd.NamedBiggest)

	// compare with the pack's reference JSON
	var ref ReplayData
	b, _ := os.ReadFile(code + "replay_data.json")
	json.Unmarshal(b, &ref)
	fmt.Println("REF days", len(ref.Days), "totals", ref.Totals, "intraday_from", ref.IntradayFrom, "breaks", len(ref.Breaks))
	if len(ref.Days) != len(rd.Days) {
		t.Errorf("day count %d vs reference %d", len(rd.Days), len(ref.Days))
	}
	mi := 0
	for i := range ref.Days {
		if i < len(rd.Days) && (ref.Days[i].M5 != nil) != (rd.Days[i].M5 != nil) {
			mi++
		}
	}
	fmt.Println("m5 presence mismatches", mi)
	if math.Abs(rd.Tomorrow.Sigma-ref.Tomorrow.Sigma) > 0.01 {
		t.Errorf("tomorrow sigma %v vs %v", rd.Tomorrow.Sigma, ref.Tomorrow.Sigma)
	}

	names := map[string]string{}
	ids := map[string]string{}
	for i, s := range nifty50 {
		ids[s] = strconv.Itoa(1000 + i)
		names[s] = s + " Ltd"
	}
	dh.last = time.Time{}
	sd := BuildScanner(dh, names, ids, now.AddDate(-5, 0, 0), now.AddDate(0, 0, 1), nil)
	fmt.Println("scanner rows", len(sd.Rows), "skipped", sd.Skipped, "asof", sd.AsOf, "first", sd.Rows[0].Symbol, sd.Rows[0].WidePc)
	if len(sd.Rows) != 50 {
		t.Errorf("scanner rows %d", len(sd.Rows))
	}
}
