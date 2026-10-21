package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const nseEquityListURL = "https://nsearchives.nseindia.com/content/equities/EQUITY_L.csv"
const bseActiveEquityURL = "https://api.bseindia.com/BseIndiaAPI/api/ListofScripData/w?Group=&Scripcode=&industry=&segment=Equity&status=Active"

// FetchYahooUniverse builds a daily-refreshed list of NSE EQ-series stocks and
// BSE active-equity scrips. NSE listings take precedence when the same ISIN is
// listed on both exchanges. BSE-only tickers use the BSE scrip code + .BO.
func FetchYahooUniverse(cachePath string) ([]YahooStock, []string, error) {
	var warnings []string
	if b, err := os.ReadFile(cachePath); err == nil {
		var cached struct {
			Fetched  time.Time    `json:"fetched"`
			Stocks   []YahooStock `json:"stocks"`
			Warnings []string     `json:"warnings"`
		}
		if json.Unmarshal(b, &cached) == nil && len(cached.Stocks) > 100 && time.Since(cached.Fetched) < 24*time.Hour {
			return cached.Stocks, cached.Warnings, nil
		}
	}
	client := &http.Client{Timeout: 25 * time.Second}
	stocks := make([]YahooStock, 0, 6000)
	byISIN := map[string]bool{}
	byTicker := map[string]bool{}

	// Official NSE cash-equity list. Restrict to EQ series (ordinary equity shares).
	req, _ := http.NewRequest(http.MethodGet, nseEquityListURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/125 Safari/537.36")
	req.Header.Set("Accept", "text/csv,*/*")
	resp, err := client.Do(req)
	if err == nil {
		if resp.StatusCode == http.StatusOK {
			reader := csv.NewReader(resp.Body)
			reader.FieldsPerRecord = -1
			rows, readErr := reader.ReadAll()
			if readErr == nil && len(rows) > 1 {
				head := map[string]int{}
				for i, h := range rows[0] {
					head[headerKey(h)] = i
				}
				for _, row := range rows[1:] {
					sym := field(row, head, "SYMBOL")
					name := field(row, head, "NAME OF COMPANY")
					series := strings.ToUpper(field(row, head, "SERIES"))
					isin := strings.ToUpper(field(row, head, "ISIN NUMBER"))
					if sym == "" || series != "EQ" {
						continue
					}
					sym = strings.ToUpper(strings.TrimSpace(sym))
					ticker := sym + ".NS"
					if byTicker[ticker] {
						continue
					}
					stocks = append(stocks, YahooStock{Symbol: sym, Name: name, Ticker: ticker, Exchange: "NSE", ISIN: isin})
					byTicker[ticker] = true
					if isin != "" {
						byISIN[isin] = true
					}
				}
			} else {
				warnings = append(warnings, "NSE list parse failed: "+fmt.Sprint(readErr))
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("NSE list HTTP %d", resp.StatusCode))
		}
		resp.Body.Close()
	} else {
		warnings = append(warnings, "NSE list unavailable: "+err.Error())
	}

	// BSE's active-equity list is a secondary source, mainly to add BSE-exclusive scrips.
	req, _ = http.NewRequest(http.MethodGet, bseActiveEquityURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/125 Safari/537.36")
	req.Header.Set("Accept", "application/json,text/plain,*/*")
	req.Header.Set("Referer", "https://www.bseindia.com/")
	resp, err = client.Do(req)
	if err == nil {
		if resp.StatusCode == http.StatusOK {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
			if readErr == nil {
				raw := decodeRecordList(body)
				if len(raw) > 0 {
					for _, item := range raw {
						code := firstValue(item, "SCRIP_CD", "SCRIPCODE", "SCRIP_CODE", "SCRIPCD", "SCRIP CODE")
						name := firstValue(item, "Scrip_Name", "SCRIP_NAME", "SCRIPNAME", "Security_Name", "Security Name")
						isin := strings.ToUpper(firstValue(item, "ISIN_NO", "ISIN", "ISIN_NUMBER", "ISIN NUMBER"))
						code = strings.TrimSpace(code)
						if code == "" || strings.EqualFold(code, "null") {
							continue
						}
						// BSE codes are numeric; nonnumeric instruments are not ordinary equity scrips.
						if _, e := strconv.Atoi(code); e != nil {
							continue
						}
						if isin != "" && byISIN[isin] {
							continue
						}
						ticker := code + ".BO"
						if byTicker[ticker] {
							continue
						}
						sym := "BSE:" + code
						stocks = append(stocks, YahooStock{Symbol: sym, Name: name, Ticker: ticker, Exchange: "BSE", ISIN: isin})
						byTicker[ticker] = true
						if isin != "" {
							byISIN[isin] = true
						}
					}
				} else {
					warnings = append(warnings, "BSE list returned an unexpected response; BSE-exclusive scrips may be missing")
				}
			} else {
				warnings = append(warnings, "BSE list read failed: "+readErr.Error())
			}
		} else {
			warnings = append(warnings, fmt.Sprintf("BSE list HTTP %d; BSE-exclusive scrips may be missing", resp.StatusCode))
		}
		resp.Body.Close()
	} else {
		warnings = append(warnings, "BSE list unavailable; BSE-exclusive scrips may be missing: "+err.Error())
	}

	if len(stocks) == 0 {
		// If no live exchange list is available, reuse a previous cached universe even if stale.
		if b, e := os.ReadFile(cachePath); e == nil {
			var cached struct {
				Stocks   []YahooStock `json:"stocks"`
				Warnings []string     `json:"warnings"`
			}
			if json.Unmarshal(b, &cached) == nil && len(cached.Stocks) > 0 {
				return cached.Stocks, append(cached.Warnings, warnings...), nil
			}
		}
		return nil, warnings, fmt.Errorf("could not load the official NSE/BSE equity lists")
	}
	sort.Slice(stocks, func(i, j int) bool { return stocks[i].Symbol < stocks[j].Symbol })
	if b, e := json.MarshalIndent(struct {
		Fetched  time.Time    `json:"fetched"`
		Stocks   []YahooStock `json:"stocks"`
		Warnings []string     `json:"warnings"`
	}{time.Now(), stocks, warnings}, "", "  "); e == nil {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0700)
		_ = os.WriteFile(cachePath, b, 0600)
	}
	return stocks, warnings, nil
}

func headerKey(s string) string {
	return strings.ToUpper(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}
func field(row []string, header map[string]int, key string) string {
	i, ok := header[headerKey(key)]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}
func firstValue(m map[string]any, keys ...string) string {
	for k, v := range m {
		for _, want := range keys {
			if strings.EqualFold(strings.ReplaceAll(strings.ReplaceAll(k, "_", ""), " ", ""), strings.ReplaceAll(strings.ReplaceAll(want, "_", ""), " ", "")) {
				switch x := v.(type) {
				case string:
					if strings.TrimSpace(x) != "" {
						return strings.TrimSpace(x)
					}
				case float64:
					if x > 0 {
						return strconv.FormatInt(int64(x), 10)
					}
				}
			}
		}
	}
	return ""
}

// decodeRecordList tolerates either a top-level JSON array or a common wrapper object
// containing an array such as Table, data, or results.
func decodeRecordList(body []byte) []map[string]any {
	var value any
	if json.Unmarshal(body, &value) != nil {
		return nil
	}
	var walk func(any) []map[string]any
	walk = func(v any) []map[string]any {
		switch x := v.(type) {
		case []any:
			out := make([]map[string]any, 0, len(x))
			for _, item := range x {
				if m, ok := item.(map[string]any); ok {
					out = append(out, m)
				}
			}
			if len(out) > 0 {
				return out
			}
		case map[string]any:
			for _, key := range []string{"Table", "table", "data", "Data", "results", "Result", "ScripData"} {
				if child, ok := x[key]; ok {
					if rows := walk(child); len(rows) > 0 {
						return rows
					}
				}
			}
		}
		return nil
	}
	return walk(value)
}
