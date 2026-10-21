// Yahoo Finance chart-endpoint adapter. This is an unofficial, best-effort source
// for research/prototyping, not a guaranteed real-time or licensed exchange feed.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type Yahoo struct {
	http *http.Client
}

func NewYahoo() *Yahoo {
	return &Yahoo{http: &http.Client{Timeout: 35 * time.Second}}
}

type yahooChartResponse struct {
	Chart struct {
		Result []struct {
			Timestamp  []int64 `json:"timestamp"`
			Indicators struct {
				Quote []struct {
					Open   []*float64 `json:"open"`
					High   []*float64 `json:"high"`
					Low    []*float64 `json:"low"`
					Close  []*float64 `json:"close"`
					Volume []*float64 `json:"volume"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func (y *Yahoo) candles(symbol, interval string, from, to time.Time) ([]Candle, error) {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil, fmt.Errorf("empty Yahoo Finance ticker")
	}
	if interval == "5m" {
		// Yahoo's chart endpoint typically limits 5-minute candles to roughly 60 days.
		limit := to.AddDate(0, 0, -59)
		if from.Before(limit) {
			from = limit
		}
	}
	if !from.Before(to) {
		return nil, fmt.Errorf("invalid date range for %s", symbol)
	}
	period2 := to.Add(24 * time.Hour)
	q := url.Values{}
	q.Set("period1", fmt.Sprintf("%d", from.Unix()))
	q.Set("period2", fmt.Sprintf("%d", period2.Unix()))
	q.Set("interval", interval)
	q.Set("events", "history")
	q.Set("includeAdjustedClose", "true")
	var lastErr error
	for _, host := range []string{"query1.finance.yahoo.com", "query2.finance.yahoo.com"} {
		endpoint := "https://" + host + "/v8/finance/chart/" + url.PathEscape(symbol) + "?" + q.Encode()
		for attempt := 0; attempt < 3; attempt++ {
			req, err := http.NewRequest(http.MethodGet, endpoint, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/125 Safari/537.36")
			req.Header.Set("Accept", "application/json,text/plain,*/*")
			resp, err := y.http.Do(req)
			if err != nil {
				lastErr = err
				time.Sleep(time.Duration(attempt+1) * 400 * time.Millisecond)
				continue
			}
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
				continue
			}
			if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
				lastErr = fmt.Errorf("Yahoo Finance HTTP %d for %s", resp.StatusCode, symbol)
				time.Sleep(time.Duration(attempt+1) * 700 * time.Millisecond)
				continue
			}
			if resp.StatusCode != http.StatusOK {
				lastErr = fmt.Errorf("Yahoo Finance HTTP %d for %s (ticker may be unsupported)", resp.StatusCode, symbol)
				break
			}
			var data yahooChartResponse
			if err := json.Unmarshal(body, &data); err != nil {
				lastErr = fmt.Errorf("Yahoo Finance returned invalid JSON for %s: %w", symbol, err)
				break
			}
			if data.Chart.Error != nil {
				lastErr = fmt.Errorf("Yahoo Finance %s: %s", data.Chart.Error.Code, data.Chart.Error.Description)
				break
			}
			if len(data.Chart.Result) == 0 {
				lastErr = fmt.Errorf("Yahoo Finance returned no candles for %s", symbol)
				break
			}
			res := data.Chart.Result[0]
			if len(res.Indicators.Quote) == 0 {
				return nil, fmt.Errorf("Yahoo Finance returned no OHLC data for %s", symbol)
			}
			quote := res.Indicators.Quote[0]
			out := make([]Candle, 0, len(res.Timestamp))
			for i, ts := range res.Timestamp {
				if i >= len(quote.Open) || i >= len(quote.High) || i >= len(quote.Low) || i >= len(quote.Close) {
					continue
				}
				if quote.Open[i] == nil || quote.High[i] == nil || quote.Low[i] == nil || quote.Close[i] == nil || *quote.Close[i] <= 0 {
					continue
				}
				c := Candle{T: time.Unix(ts, 0).In(IST), O: *quote.Open[i], H: *quote.High[i], L: *quote.Low[i], C: *quote.Close[i]}
				if i < len(quote.Volume) && quote.Volume[i] != nil {
					c.Volume = *quote.Volume[i]
				}
				out = append(out, c)
			}
			sort.Slice(out, func(i, j int) bool { return out[i].T.Before(out[j].T) })
			// De-duplicate by timestamp and reject empty results so failures are visible.
			seen := map[int64]bool{}
			unique := out[:0]
			for _, c := range out {
				k := c.T.Unix()
				if !seen[k] {
					seen[k] = true
					unique = append(unique, c)
				}
			}
			if len(unique) == 0 {
				return nil, fmt.Errorf("Yahoo Finance returned no usable candles for %s (%s)", symbol, interval)
			}
			return unique, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("Yahoo Finance request failed for %s", symbol)
	}
	return nil, lastErr
}

func (y *Yahoo) Daily(symbol string, from, to time.Time) ([]Candle, error) {
	return y.candles(symbol, "1d", from, to)
}

func (y *Yahoo) Intraday5(symbol string, from, to time.Time, progress func(float64)) ([]Candle, error) {
	cs, err := y.candles(symbol, "5m", from, to)
	if progress != nil {
		progress(1)
	}
	return cs, err
}
