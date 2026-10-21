// Dhan API access. Read-only: this file only ever calls Dhan's historical-candle endpoints and
// downloads Dhan's public instrument list. This software never places an order.
// Your access token is sent only to api.dhan.co.
package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

var IST = time.FixedZone("IST", 5*3600+1800)

var (
	dhanBase  = "https://api.dhan.co/v2"
	masterURL = "https://images.dhan.co/api-data/api-scrip-master.csv"
)

type Candle struct {
	T                  time.Time // in IST
	O, H, L, C, Volume float64
}

type Dhan struct {
	ClientID, Token string
	http            *http.Client
	last            time.Time
}

func NewDhan(clientID, token string) *Dhan {
	return &Dhan{ClientID: clientID, Token: token, http: &http.Client{Timeout: 90 * time.Second}}
}

// post sends one JSON request with polite pacing (Dhan allows ~5 data calls a second) and retries
// on rate limits / temporary server errors.
func (d *Dhan) post(path string, body map[string]any) ([]byte, int, error) {
	b, _ := json.Marshal(body)
	var raw []byte
	var code int
	for attempt := 0; attempt < 4; attempt++ {
		if w := 230*time.Millisecond - time.Since(d.last); w > 0 {
			time.Sleep(w)
		}
		d.last = time.Now()
		req, _ := http.NewRequest("POST", dhanBase+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("access-token", d.Token)
		if d.ClientID != "" {
			req.Header.Set("client-id", d.ClientID)
		}
		resp, err := d.http.Do(req)
		if err != nil {
			if attempt == 3 {
				return nil, 0, fmt.Errorf("cannot reach Dhan (%v) - check your internet connection", err)
			}
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		raw, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		code = resp.StatusCode
		if code == 429 || code >= 500 {
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		break
	}
	return raw, code, nil
}

type candleResp struct {
	Open, High, Low, Close, Volume, Timestamp []float64
}

func parseCandles(raw []byte, code int) ([]Candle, error) {
	var d candleResp
	var probe struct {
		Open      []float64 `json:"open"`
		High      []float64 `json:"high"`
		Low       []float64 `json:"low"`
		Close     []float64 `json:"close"`
		Volume    []float64 `json:"volume"`
		Timestamp []float64 `json:"timestamp"`
		ErrType   string    `json:"errorType"`
		ErrCode   string    `json:"errorCode"`
		ErrMsg    string    `json:"errorMessage"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		if code == 401 || code == 403 {
			return nil, errors.New("Dhan says the access token is invalid or expired - paste a fresh one under Settings")
		}
		return nil, fmt.Errorf("Dhan API %d: %.160s", code, string(raw))
	}
	if probe.Timestamp == nil && code == 200 && probe.ErrCode == "" && probe.ErrMsg == "" {
		return nil, nil // an empty range (e.g. a window before the stock existed)
	}
	if probe.Timestamp == nil {
		if code == 401 || probe.ErrCode == "DH-901" || probe.ErrCode == "DH-902" {
			return nil, errors.New("Dhan says the access token is invalid or expired - paste a fresh one under Settings")
		}
		if probe.ErrMsg != "" {
			return nil, fmt.Errorf("Dhan API %d %s: %s", code, probe.ErrCode, probe.ErrMsg)
		}
		return nil, fmt.Errorf("Dhan API %d: %.160s", code, string(raw))
	}
	_ = d
	n := len(probe.Timestamp)
	out := make([]Candle, 0, n)
	for i := 0; i < n; i++ {
		if i >= len(probe.Close) || i >= len(probe.Open) || i >= len(probe.High) || i >= len(probe.Low) || probe.Close[i] <= 0 {
			continue
		}
		c := Candle{T: time.Unix(int64(probe.Timestamp[i]), 0).In(IST), O: probe.Open[i], H: probe.High[i], L: probe.Low[i], C: probe.Close[i]}
		if i < len(probe.Volume) {
			c.Volume = probe.Volume[i]
		}
		out = append(out, c)
	}
	return out, nil
}

func isNoData(err error) bool { return err != nil && strings.Contains(err.Error(), "DH-907") }

// dailyWindow asks for one window, then falls back step by step if Dhan answers "DH-907" (bad
// parameters / no data): first without any future date, then in one-year slices. A window with no
// data is simply empty - only a window-less result is treated as an error by the caller.
func (d *Dhan) dailyWindow(secID, segment, instrument string, from, to time.Time) ([]Candle, error) {
	ask := func(a, b time.Time) ([]Candle, error) {
		raw, code, err := d.post("/charts/historical", map[string]any{"securityId": secID, "exchangeSegment": segment,
			"instrument": instrument, "expiryCode": 0, "oi": false,
			"fromDate": a.Format("2006-01-02"), "toDate": b.Format("2006-01-02")})
		if err != nil {
			return nil, err
		}
		return parseCandles(raw, code)
	}
	cs, err := ask(from, to)
	if !isNoData(err) {
		return cs, err
	}
	today := time.Now().In(IST)
	if to.After(today) {
		if cs, err = ask(from, today); !isNoData(err) {
			return cs, err
		}
		to = today
	}
	var all []Candle
	for cur := from; cur.Before(to); {
		end := cur.AddDate(1, 0, 0)
		if end.After(to) {
			end = to
		}
		part, perr := ask(cur, end)
		if perr != nil && !isNoData(perr) {
			return nil, perr
		}
		all = append(all, part...)
		cur = end
	}
	if len(all) == 0 {
		return nil, err
	}
	return all, nil
}

// Daily candles, fetched in 3-year windows and stitched together (oldest first, one per date).
func (d *Dhan) Daily(secID, segment, instrument string, from, to time.Time) ([]Candle, error) {
	var all []Candle
	var lastNoData error
	for cur := from; cur.Before(to); {
		end := cur.AddDate(3, 0, 0)
		if end.After(to) {
			end = to
		}
		cs, err := d.dailyWindow(secID, segment, instrument, cur, end)
		if isNoData(err) {
			lastNoData, err = err, nil
		}
		if err != nil {
			return nil, err
		}
		all = append(all, cs...)
		cur = end
	}
	if len(all) == 0 && lastNoData != nil {
		return nil, fmt.Errorf("%v - Dhan returned no candles. Check that the client id belongs to this access token and that your Dhan Data API is active", lastNoData)
	}
	seen := map[string]bool{}
	out := all[:0]
	sort.Slice(all, func(i, j int) bool { return all[i].T.Before(all[j].T) })
	for _, c := range all {
		k := c.T.Format("2006-01-02")
		if !seen[k] {
			seen[k] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// Intraday 5-minute candles. Dhan serves at most ~90 days per call, so page in 80-day windows.
// A window Dhan has no data for is skipped; token problems stop the run.
func (d *Dhan) Intraday5(secID, segment, instrument string, from, to time.Time, progress func(float64)) ([]Candle, error) {
	var all []Candle
	total := to.Sub(from).Hours()
	if now := time.Now().In(IST); to.After(now) {
		to = now
	}
	for cur := from; cur.Before(to); {
		end := cur.AddDate(0, 0, 80)
		if end.After(to) {
			end = to
		}
		raw, code, err := d.post("/charts/intraday", map[string]any{"securityId": secID, "exchangeSegment": segment,
			"instrument": instrument, "interval": "5", "oi": false,
			"fromDate": cur.Format("2006-01-02 15:04:05"), "toDate": end.Format("2006-01-02 15:04:05")})
		if err != nil {
			return nil, err
		}
		cs, err := parseCandles(raw, code)
		if err != nil && !isNoData(err) {
			return nil, err
		}
		all = append(all, cs...)
		cur = end
		if progress != nil {
			progress(1 - to.Sub(cur).Hours()/total)
		}
	}
	seen := map[int64]bool{}
	out := all[:0]
	sort.Slice(all, func(i, j int) bool { return all[i].T.Before(all[j].T) })
	for _, c := range all {
		if !seen[c.T.Unix()] {
			seen[c.T.Unix()] = true
			out = append(out, c)
		}
	}
	return out, nil
}

// SecurityIDs downloads Dhan's public instrument list and returns NSE cash-equity security ids by
// trading symbol.
func SecurityIDs(symbols []string) (map[string]string, error) {
	resp, err := (&http.Client{Timeout: 120 * time.Second}).Get(masterURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("instrument list: HTTP %d", resp.StatusCode)
	}
	rd := csv.NewReader(resp.Body)
	rd.FieldsPerRecord = -1
	rd.LazyQuotes = true
	head, err := rd.Read()
	if err != nil {
		return nil, err
	}
	col := func(names ...string) int {
		for _, n := range names {
			for i, h := range head {
				if strings.EqualFold(strings.TrimSpace(h), n) {
					return i
				}
			}
		}
		return -1
	}
	cEx := col("SEM_EXM_EXCH_ID", "EXCH_ID")
	cSeg := col("SEM_SEGMENT", "SEGMENT")
	cID := col("SEM_SMST_SECURITY_ID", "SECURITY_ID")
	cInst := col("SEM_INSTRUMENT_NAME", "INSTRUMENT")
	cSym := col("SEM_TRADING_SYMBOL", "UNDERLYING_SYMBOL", "SYMBOL_NAME")
	cSer := col("SEM_SERIES", "SERIES")
	if cEx < 0 || cID < 0 || cSym < 0 {
		return nil, errors.New("instrument list has an unexpected layout")
	}
	want := map[string]bool{}
	for _, s := range symbols {
		want[strings.ToUpper(s)] = true
	}
	out := map[string]string{}
	for {
		rec, err := rd.Read()
		if err == io.EOF {
			break
		}
		if err != nil || len(rec) <= cSym {
			continue
		}
		get := func(i int) string {
			if i < 0 || i >= len(rec) {
				return ""
			}
			return strings.TrimSpace(rec[i])
		}
		if get(cEx) != "NSE" || (cSeg >= 0 && get(cSeg) != "E") || (cInst >= 0 && get(cInst) != "EQUITY") ||
			(cSer >= 0 && get(cSer) != "EQ") {
			continue
		}
		sym := strings.ToUpper(get(cSym))
		if want[sym] {
			out[sym] = get(cID)
		}
	}
	return out, nil
}
