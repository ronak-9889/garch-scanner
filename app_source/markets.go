package main

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Global benchmark indices are monitored separately from the NSE/BSE stock universe.
// Yahoo Finance tickers are best-effort and may be unavailable or delayed.
type MarketIndex struct {
	Symbol    string    `json:"symbol"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	Close     float64   `json:"close"`
	Previous  float64   `json:"previous_close"`
	Change    float64   `json:"change_pct"`
	AsOf      string    `json:"as_of"`
	Status    string    `json:"status"`
	Error     string    `json:"error,omitempty"`
	Forecasts []Horizon `json:"forecasts,omitempty"`
}

type MarketsData struct {
	Source    string        `json:"source"`
	FetchedAt string        `json:"fetched_at"`
	Indices   []MarketIndex `json:"indices"`
}

var globalIndices = []struct{ Symbol, Name, Region string }{
	{"^TWII", "Taiwan Weighted Index (TAIEX)", "Taiwan"},
	{"^IXIC", "Nasdaq Composite", "United States"},
	{"^GSPC", "S&P 500", "United States"},
	{"^KS11", "KOSPI", "South Korea"},
}

func BuildMarkets(yh *Yahoo, now time.Time, progress func(string)) *MarketsData {
	out := &MarketsData{Source: "live", FetchedAt: now.Format(time.RFC3339), Indices: make([]MarketIndex, 0, len(globalIndices))}
	for _, idx := range globalIndices {
		if progress != nil {
			progress(idx.Name)
		}
		row := MarketIndex{Symbol: idx.Symbol, Name: idx.Name, Region: idx.Region, Status: "unavailable"}
		// Fit the same GARCH(1,1) range model used by the Nifty chart and stock scanner.
		// Six years of daily history provides enough returns for a stable fit when available.
		cs, err := yh.Daily(idx.Symbol, now.AddDate(-6, 0, 0), now.AddDate(0, 0, 1))
		if err != nil {
			row.Error = err.Error()
			out.Indices = append(out.Indices, row)
			continue
		}
		cs = dropUnfinished(cs)
		if len(cs) < 2 {
			row.Error = "Not enough completed daily candles returned by Yahoo Finance"
			out.Indices = append(out.Indices, row)
			continue
		}
		last, prev := cs[len(cs)-1], cs[len(cs)-2]
		if prev.C <= 0 || last.C <= 0 || math.IsNaN(prev.C) || math.IsNaN(last.C) || math.IsInf(prev.C, 0) || math.IsInf(last.C, 0) {
			row.Error = "Invalid closing prices returned by Yahoo Finance"
			out.Indices = append(out.Indices, row)
			continue
		}
		row.Close, row.Previous = last.C, prev.C
		row.Change = (last.C/prev.C - 1) * 100
		if math.IsNaN(row.Change) || math.IsInf(row.Change, 0) {
			row.Close, row.Previous, row.Change = 0, 0, 0
			row.Error = "Invalid percentage change"
			out.Indices = append(out.Indices, row)
			continue
		}
		// Build percent log returns, fit GARCH, then produce tomorrow/week/month ranges.
		returns := make([]float64, 0, len(cs)-1)
		for i := 1; i < len(cs); i++ {
			if cs[i-1].C > 0 && cs[i].C > 0 && !math.IsNaN(cs[i-1].C) && !math.IsNaN(cs[i].C) && !math.IsInf(cs[i-1].C, 0) && !math.IsInf(cs[i].C, 0) {
				returns = append(returns, 100*math.Log(cs[i].C/cs[i-1].C))
			}
		}
		if len(returns) < 100 {
			row.Error = "Price history is available, but fewer than 100 valid daily returns were returned; forecast ranges cannot be fitted"
			out.Indices = append(out.Indices, row)
			continue
		}
		params, fitErr := FitGARCH11(returns)
		if fitErr != nil {
			row.Error = "Could not fit GARCH range model: " + fitErr.Error()
			out.Indices = append(out.Indices, row)
			continue
		}
		sigma := Forecast1(returns, params)
		if sigma <= 0 || math.IsNaN(sigma) || math.IsInf(sigma, 0) {
			row.Error = "GARCH model returned an invalid volatility forecast"
			out.Indices = append(out.Indices, row)
			continue
		}
		row.Forecasts = makeHorizons(params, last.C, sigma)
		row.AsOf, row.Status = last.T.Format("2006-01-02"), "available"
		out.Indices = append(out.Indices, row)
	}
	sort.SliceStable(out.Indices, func(i, j int) bool { return out.Indices[i].Region < out.Indices[j].Region })
	return out
}

func marketsSummary(m *MarketsData) string {
	if m == nil {
		return "global indices not loaded"
	}
	ok := 0
	for _, i := range m.Indices {
		if i.Status == "available" {
			ok++
		}
	}
	return fmt.Sprintf("global index monitor: %d of %d indices available", ok, len(m.Indices))
}
