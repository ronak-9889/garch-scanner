// The whole refresh, run every time the app opens:
//  1. Nifty 50 daily candles (6 years) + 5-minute candles (last 365 days) from Dhan
//  2. GARCH(1,1) fit on daily closes; honest 3-year out-of-sample test (refit every 21 days)
//  3. tomorrow's two fences
//  4. the same GARCH fit for each Nifty 50 stock (the scanner)
//
// Results are the same JSON shapes the replay page and the scanner page read.
package main

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	niftySecID, niftySeg, niftyInst = "13", "IDX_I", "INDEX"
	oosWindow                       = 756 // three years of trading days
	oosRefit                        = 21
	lead                            = 40 // candles shown before the first forecast
)

// ---------- replay json ----------

type Day struct {
	D   string       `json:"d"`
	O   float64      `json:"o"`
	H   float64      `json:"h"`
	L   float64      `json:"l"`
	C   float64      `json:"c"`
	S   *float64     `json:"s,omitempty"`
	Ret *float64     `json:"ret,omitempty"`
	Mu  *float64     `json:"mu,omitempty"`
	In1 *bool        `json:"in1,omitempty"`
	In2 *bool        `json:"in2,omitempty"`
	Lo1 *float64     `json:"lo1,omitempty"`
	Hi1 *float64     `json:"hi1,omitempty"`
	Lo2 *float64     `json:"lo2,omitempty"`
	Hi2 *float64     `json:"hi2,omitempty"`
	M5  [][5]float64 `json:"m5,omitempty"`
}

type Break struct {
	D     string  `json:"d"`
	Ret   float64 `json:"ret"`
	S     float64 `json:"s"`
	Label string  `json:"label"`
}

type Tomorrow struct {
	Sigma float64 `json:"sigma"`
	Close float64 `json:"close"`
	From  string  `json:"from"`
	Lo1   float64 `json:"lo1"`
	Hi1   float64 `json:"hi1"`
	Lo2   float64 `json:"lo2"`
	Hi2   float64 `json:"hi2"`
	Dn1   float64 `json:"dn1"`
	Up1   float64 `json:"up1"`
	Dn2   float64 `json:"dn2"`
	Up2   float64 `json:"up2"`
}

type ParamsOut struct {
	Mu       float64 `json:"mu"`
	Omega    float64 `json:"omega"`
	Alpha    float64 `json:"alpha"`
	Beta     float64 `json:"beta"`
	Persist  float64 `json:"persistence"`
	HalfLife float64 `json:"half_life_days"`
	Uncond   float64 `json:"unconditional_sigma_pct"`
	LogLik   float64 `json:"log_likelihood"`
}

type Check struct {
	Days       int     `json:"days"`
	Inside68   float64 `json:"inside_68_band"`
	Inside95   float64 `json:"inside_95_band"`
	Inside95HV float64 `json:"inside_95_band_hv20"`
	MAEGarch   float64 `json:"vol_mae_garch"`
	MAEHV      float64 `json:"vol_mae_hv20"`
}

type Totals struct {
	N   int `json:"n"`
	In1 int `json:"in1"`
	In2 int `json:"in2"`
}

// Horizon: the same GARCH fence, for the next 1, 5 (a week) or 21 (a month) trading days.
type HCheck struct {
	N   int `json:"n"`
	In1 int `json:"in1"`
	In2 int `json:"in2"`
}

type Horizon struct {
	Days  int     `json:"days"`
	Label string  `json:"label"`
	Sigma float64 `json:"sigma"` // sigma of the h-day move, %
	Inner float64 `json:"inner"` // +/- % either side, inner fence (1 sigma)
	Wide  float64 `json:"wide"`  // +/- % either side, wide fence (1.96 sigma)
	Lo1   float64 `json:"lo1"`
	Hi1   float64 `json:"hi1"`
	Lo2   float64 `json:"lo2"`
	Hi2   float64 `json:"hi2"`
	Check *HCheck `json:"check,omitempty"`
}

var horizonDays = []struct {
	Days  int
	Label string
}{{1, "Tomorrow"}, {5, "Next week"}, {21, "Next month"}}

// makeHorizons builds the fences for each horizon from the fitted parameters and tomorrow's sigma.
func makeHorizons(p Params, close, sig1 float64) []Horizon {
	out := []Horizon{}
	for _, hd := range horizonDays {
		s := HorizonSigma(p, sig1*sig1, hd.Days)
		c := p.Mu * float64(hd.Days)
		out = append(out, Horizon{Days: hd.Days, Label: hd.Label, Sigma: s, Inner: s, Wide: 1.96 * s,
			Lo1: close * math.Exp((c-s)/100), Hi1: close * math.Exp((c+s)/100),
			Lo2: close * math.Exp((c-1.96*s)/100), Hi2: close * math.Exp((c+1.96*s)/100)})
	}
	return out
}

// PRow: one weekly / monthly row for the chart. The candles inside it are the DAILY candles of that
// period, each [index in period, o, h, l, c, "date"], drawn inside the period's own fence.
type PRow struct {
	D   string  `json:"d"`
	O   float64 `json:"o"`
	H   float64 `json:"h"`
	L   float64 `json:"l"`
	C   float64 `json:"c"`
	S   float64 `json:"s"`
	Ret float64 `json:"ret"`
	Mu  float64 `json:"mu"`
	In1 bool    `json:"in1"`
	In2 bool    `json:"in2"`
	Lo1 float64 `json:"lo1"`
	Hi1 float64 `json:"hi1"`
	Lo2 float64 `json:"lo2"`
	Hi2 float64 `json:"hi2"`
	M5  [][]any `json:"m5"`
}

type Period struct {
	Days     int      `json:"days"`
	Rows     []PRow   `json:"rows"`
	Tomorrow Tomorrow `json:"tomorrow"`
	Totals   Totals   `json:"totals"`
	Breaks   []Break  `json:"breaks"`
	Biggest  *Break   `json:"biggest_break"`
}

type ReplayData struct {
	Days         []Day              `json:"days"`
	Tomorrow     Tomorrow           `json:"tomorrow"`
	Params       ParamsOut          `json:"params"`
	Check        Check              `json:"check"`
	Breaks       []Break            `json:"breaks"`
	Biggest      *Break             `json:"biggest_break"`
	NamedBiggest *Break             `json:"named_biggest_break"`
	DataFrom     string             `json:"data_from"`
	DataTo       string             `json:"data_to"`
	IntradayFrom string             `json:"intraday_from"`
	Totals       Totals             `json:"totals"`
	Horizons     []Horizon          `json:"horizons,omitempty"`
	Periods      map[string]*Period `json:"periods,omitempty"` // "weeks" (5 trading days) and "months" (21)
	Source       string             `json:"source"`
	FetchedAt    string             `json:"fetched_at"`
}

// Only events we can name for certain; everything else stays unlabelled.
var breakLabels = map[string]string{"2024-06-04": "Election result day", "2025-04-07": "US tariff shock", "2025-05-12": "Ceasefire rally"}

// ---------- scanner json ----------

type ScanRow struct {
	Symbol string  `json:"symbol"`
	Name   string  `json:"name"`
	Close  float64 `json:"close"`
	Sigma  float64 `json:"sigma_pct"`
	Lo1    float64 `json:"lo1"`
	Hi1    float64 `json:"hi1"`
	Lo2    float64 `json:"lo2"`
	Hi2    float64 `json:"hi2"`
	WidePc float64 `json:"wide_pct"`
	AsOf   string  `json:"as_of"`
	Mu     float64 `json:"mu,omitempty"`
	Sig5   float64 `json:"sigma5_pct,omitempty"`  // sigma of the next 5 trading days (a week)
	Sig21  float64 `json:"sigma21_pct,omitempty"` // sigma of the next 21 trading days (a month)
}

type ScanData struct {
	AsOf      string    `json:"as_of"`
	Rows      []ScanRow `json:"rows"`
	Skipped   []string  `json:"skipped,omitempty"`
	Source    string    `json:"source"`
	FetchedAt string    `json:"fetched_at"`
}

var nifty50 = []string{"ADANIENT", "ADANIPORTS", "APOLLOHOSP", "ASIANPAINT", "AXISBANK", "BAJAJ-AUTO", "BAJFINANCE",
	"BAJAJFINSV", "BEL", "BHARTIARTL", "CIPLA", "COALINDIA", "DRREDDY", "EICHERMOT", "ETERNAL", "GRASIM",
	"HCLTECH", "HDFCBANK", "HDFCLIFE", "HINDALCO", "HINDUNILVR", "ICICIBANK", "ITC", "INFY", "INDIGO",
	"JSWSTEEL", "JIOFIN", "KOTAKBANK", "LT", "M&M", "MARUTI", "MAXHEALTH", "NTPC", "NESTLEIND", "ONGC",
	"POWERGRID", "RELIANCE", "SBILIFE", "SHRIRAMFIN", "SBIN", "SUNPHARMA", "TCS", "TATACONSUM", "TMPV",
	"TATASTEEL", "TECHM", "TITAN", "TRENT", "ULTRACEMCO", "WIPRO"}

// ---------- helpers ----------

// dropUnfinished removes today's candle until the market has closed, so the model only ever sees
// completed days.
func dropUnfinished(cs []Candle) []Candle {
	if len(cs) == 0 {
		return cs
	}
	now := time.Now().In(IST)
	last := cs[len(cs)-1].T
	if last.Format("2006-01-02") == now.Format("2006-01-02") && now.Hour()*60+now.Minute() < 15*60+45 {
		return cs[:len(cs)-1]
	}
	return cs
}

func r2(x float64) float64 { return math.Round(x*100) / 100 }
func rn(x float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(x*p) / p
}
func fp(x float64) *float64 { return &x }
func bp(x bool) *bool       { return &x }

func bands(close, mu, sig float64) (lo1, hi1, lo2, hi2 float64) {
	lo1 = close * math.Exp((mu-sig)/100)
	hi1 = close * math.Exp((mu+sig)/100)
	lo2 = close * math.Exp((mu-1.96*sig)/100)
	hi2 = close * math.Exp((mu+1.96*sig)/100)
	return
}

// ---------- Nifty replay ----------

func BuildReplay(daily []Candle, intra []Candle, now time.Time) (*ReplayData, error) {
	if len(daily) < oosWindow+300 {
		return nil, fmt.Errorf("only %d days of Nifty history came back from Dhan - need about %d", len(daily), oosWindow+300)
	}
	closes := make([]float64, len(daily))
	dates := make([]string, len(daily))
	for i, c := range daily {
		closes[i], dates[i] = c.C, c.T.Format("2006-01-02")
	}
	r := pctReturns(closes) // r[k] is the return of day k+1
	p, err := FitGARCH11(r)
	if err != nil {
		return nil, err
	}
	oos, err := RollingOOS(r, oosWindow, oosRefit)
	if err != nil {
		return nil, err
	}

	persist := p.Alpha + p.Beta
	sig1 := Forecast1(r, p)
	lastClose := closes[len(closes)-1]
	lo1, hi1, lo2, hi2 := bands(lastClose, p.Mu, sig1)

	// 5-minute bars grouped by date, only for dates that also have a daily candle
	dayIdx := map[string]int{}
	for i, d := range dates {
		dayIdx[d] = i
	}
	m5 := map[string][][5]float64{}
	for _, c := range intra {
		d := c.T.Format("2006-01-02")
		if _, ok := dayIdx[d]; !ok {
			continue
		}
		mo := c.T.Hour()*60 + c.T.Minute() - (9*60 + 15)
		if mo < 0 || mo > 375 {
			continue
		}
		m5[d] = append(m5[d], [5]float64{float64(mo), r2(c.O), r2(c.H), r2(c.L), r2(c.C)})
	}
	intradayFrom := ""
	for d, v := range m5 {
		sort.Slice(v, func(a, b int) bool { return v[a][0] < v[b][0] })
		if intradayFrom == "" || d < intradayFrom {
			intradayFrom = d
		}
	}

	oosByDay := map[int]OOSPoint{}
	for _, o := range oos {
		oosByDay[o.Idx+1] = o
	}
	firstDay := oos[0].Idx + 1
	startDay := firstDay - lead
	if startDay < 0 {
		startDay = 0
	}

	rd := &ReplayData{Source: "live", FetchedAt: now.Format(time.RFC3339), DataFrom: dates[1], DataTo: dates[len(dates)-1], IntradayFrom: intradayFrom}
	hv := make([]float64, 0, len(oos))
	var sumG, sumH float64
	var in68, in95, in95hv int
	for di := startDay; di < len(daily); di++ {
		c := daily[di]
		row := Day{D: dates[di], O: c.O, H: c.H, L: c.L, C: c.C}
		if o, ok := oosByDay[di]; ok {
			pc := closes[di-1]
			ret := r[di-1]
			dev := math.Abs(ret - o.Mu)
			l1, h1, l2, h2 := bands(pc, o.Mu, o.Sigma)
			row.S, row.Ret, row.Mu = fp(rn(o.Sigma, 4)), fp(rn(ret, 3)), fp(rn(o.Mu, 4))
			row.In1, row.In2 = bp(dev <= o.Sigma), bp(dev <= 1.96*o.Sigma)
			row.Lo1, row.Hi1, row.Lo2, row.Hi2 = fp(l1), fp(h1), fp(l2), fp(h2)
			rd.Totals.N++
			if dev <= o.Sigma {
				rd.Totals.In1++
				in68++
			}
			if dev <= 1.96*o.Sigma {
				rd.Totals.In2++
				in95++
			}
			// 20-day historical-volatility yardstick, centred on the same mu
			k := di - 1 // index into r
			if k >= 20 {
				w := r[k-20 : k]
				m := 0.0
				for _, v := range w {
					m += v
				}
				m /= 20
				ss := 0.0
				for _, v := range w {
					ss += (v - m) * (v - m)
				}
				sd := math.Sqrt(ss / 19)
				hv = append(hv, sd)
				if dev <= 1.96*sd {
					in95hv++
				}
				sumH += math.Abs(math.Abs(ret) - sd*math.Sqrt(2/math.Pi))
			}
			sumG += math.Abs(math.Abs(ret) - o.Sigma*math.Sqrt(2/math.Pi))
			if !*row.In2 {
				rd.Breaks = append(rd.Breaks, Break{D: row.D, Ret: rn(ret, 3), S: rn(o.Sigma, 4), Label: breakLabels[row.D]})
			}
		}
		if v, ok := m5[row.D]; ok {
			row.M5 = v
		}
		rd.Days = append(rd.Days, row)
	}
	nn := float64(len(oos))
	nh := float64(len(hv))
	if nh == 0 {
		nh = 1
	}
	rd.Check = Check{Days: len(oos), Inside68: float64(in68) / nn, Inside95: float64(in95) / nn, Inside95HV: float64(in95hv) / nh,
		MAEGarch: sumG / nn, MAEHV: sumH / nh}
	bySize := append([]Break(nil), rd.Breaks...)
	sort.Slice(bySize, func(a, b int) bool { return math.Abs(bySize[a].Ret) > math.Abs(bySize[b].Ret) })
	if len(bySize) > 0 {
		b := bySize[0]
		rd.Biggest = &b
		for i := range bySize {
			if bySize[i].Label != "" {
				nb := bySize[i]
				rd.NamedBiggest = &nb
				break
			}
		}
	}
	rd.Params = ParamsOut{Mu: p.Mu, Omega: p.Omega, Alpha: p.Alpha, Beta: p.Beta, Persist: persist,
		HalfLife: math.Log(0.5) / math.Log(persist), Uncond: math.Sqrt(p.Omega / (1 - persist)), LogLik: -negLogLik(r, p)}
	rd.Tomorrow = Tomorrow{Sigma: sig1, Close: lastClose, From: dates[len(dates)-1], Lo1: lo1, Hi1: hi1, Lo2: lo2, Hi2: hi2,
		Dn1: lastClose - lo1, Up1: hi1 - lastClose, Dn2: lastClose - lo2, Up2: hi2 - lastClose}
	rd.Horizons = makeHorizons(p, lastClose, sig1)
	// weekly (5 trading days) and monthly (21) fences: non-overlapping blocks counted back from the
	// latest day, each forecast made only from data up to the day before the block starts. The same
	// blocks drive the chart's 1W / 1M views and the "tested honestly" line.
	rd.Periods = map[string]*Period{}
	for hi := range rd.Horizons {
		h := rd.Horizons[hi].Days
		if h == 1 {
			rd.Horizons[hi].Check = &HCheck{N: rd.Totals.N, In1: rd.Totals.In1, In2: rd.Totals.In2}
			continue
		}
		pd := buildPeriod(h, daily, dates, closes, r, oosByDay, firstDay)
		pd.Tomorrow = rd.Tomorrow
		hz := rd.Horizons[hi]
		pd.Tomorrow.Sigma, pd.Tomorrow.Lo1, pd.Tomorrow.Hi1, pd.Tomorrow.Lo2, pd.Tomorrow.Hi2 = hz.Sigma, hz.Lo1, hz.Hi1, hz.Lo2, hz.Hi2
		pd.Tomorrow.Dn1, pd.Tomorrow.Up1, pd.Tomorrow.Dn2, pd.Tomorrow.Up2 = lastClose-hz.Lo1, hz.Hi1-lastClose, lastClose-hz.Lo2, hz.Hi2-lastClose
		rd.Horizons[hi].Check = &HCheck{N: pd.Totals.N, In1: pd.Totals.In1, In2: pd.Totals.In2}
		key := "weeks"
		if h == 21 {
			key = "months"
		}
		rd.Periods[key] = pd
	}
	return rd, nil
}

func buildPeriod(h int, daily []Candle, dates []string, closes, r []float64, oosByDay map[int]OOSPoint, firstDay int) *Period {
	pd := &Period{Days: h}
	var rows []PRow
	for end := len(daily) - 1; end-h+1 >= firstDay; end -= h {
		start := end - h + 1
		o, ok := oosByDay[start]
		if !ok {
			break
		}
		pc := closes[start-1]
		hi, lo := daily[start].H, daily[start].L
		bars := make([][]any, 0, h)
		for k := start; k <= end; k++ {
			hi, lo = math.Max(hi, daily[k].H), math.Min(lo, daily[k].L)
			bars = append(bars, []any{k - start, daily[k].O, daily[k].H, daily[k].L, daily[k].C, dates[k]})
		}
		s := HorizonSigma(o.P, o.Sigma*o.Sigma, h)
		mu := o.Mu * float64(h)
		ret := 100 * math.Log(closes[end]/pc)
		dev := math.Abs(ret - mu)
		l1, h1, l2, h2 := pc*math.Exp((mu-s)/100), pc*math.Exp((mu+s)/100), pc*math.Exp((mu-1.96*s)/100), pc*math.Exp((mu+1.96*s)/100)
		rows = append(rows, PRow{D: dates[start], O: daily[start].O, H: hi, L: lo, C: closes[end], S: rn(s, 4), Ret: rn(ret, 3), Mu: rn(mu, 4),
			In1: dev <= s, In2: dev <= 1.96*s, Lo1: l1, Hi1: h1, Lo2: l2, Hi2: h2, M5: bars})
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 { // oldest first
		rows[i], rows[j] = rows[j], rows[i]
	}
	pd.Rows = rows
	for _, x := range rows {
		pd.Totals.N++
		if x.In1 {
			pd.Totals.In1++
		}
		if x.In2 {
			pd.Totals.In2++
		} else {
			pd.Breaks = append(pd.Breaks, Break{D: x.D, Ret: x.Ret, S: x.S})
		}
	}
	for i := range pd.Breaks {
		if pd.Biggest == nil || math.Abs(pd.Breaks[i].Ret) > math.Abs(pd.Biggest.Ret) {
			b := pd.Breaks[i]
			pd.Biggest = &b
		}
	}
	return pd
}

// ---------- scanner ----------

func BuildScanner(dh *Dhan, names map[string]string, idOf map[string]string, from, to time.Time, progress func(done, total int, sym string)) *ScanData {
	var mu sync.Mutex
	rows := []ScanRow{}
	var skipped []string
	// Dhan allows ~5 data calls a second, so fetch one stock at a time (Dhan's pacing is inside
	// Daily) and fit as we go.
	for k, sym := range nifty50 {
		if progress != nil {
			progress(k, len(nifty50), sym)
		}
		id, ok := idOf[sym]
		if !ok {
			skipped = append(skipped, sym+" (not in Dhan's instrument list)")
			continue
		}
		cs, err := dh.Daily(id, "NSE_EQ", "EQUITY", from, to)
		if err != nil {
			if strings.Contains(err.Error(), "token") {
				skipped = append(skipped, "all (token problem)")
				break
			}
			skipped = append(skipped, sym+" ("+err.Error()+")")
			continue
		}
		cs = dropUnfinished(cs)
		if len(cs) < 251 {
			skipped = append(skipped, sym+" (short history)")
			continue
		}
		closes := make([]float64, len(cs))
		for i, c := range cs {
			closes[i] = c.C
		}
		r := pctReturns(closes)
		p, err := FitGARCH11(r)
		if err != nil {
			skipped = append(skipped, sym+" (fit failed)")
			continue
		}
		sg := Forecast1(r, p)
		cl := closes[len(closes)-1]
		l1, h1, l2, h2 := bands(cl, p.Mu, sg)
		name := names[sym]
		if name == "" {
			name = sym
		}
		mu.Lock()
		rows = append(rows, ScanRow{Symbol: sym, Name: name, Close: cl, Sigma: sg, Lo1: l1, Hi1: h1, Lo2: l2, Hi2: h2,
			WidePc: (h2 - l2) / cl * 100, AsOf: cs[len(cs)-1].T.Format("2006-01-02"),
			Mu: p.Mu, Sig5: HorizonSigma(p, sg*sg, 5), Sig21: HorizonSigma(p, sg*sg, 21)})
		mu.Unlock()
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].WidePc > rows[b].WidePc })
	sd := &ScanData{Rows: rows, Skipped: skipped, Source: "live", FetchedAt: time.Now().Format(time.RFC3339)}
	if len(rows) > 0 {
		sd.AsOf = rows[0].AsOf
	}
	return sd
}

var errNoKeys = errors.New("no Dhan keys saved yet")

// BuildYahooScanner applies the same GARCH(1,1) model and bands as BuildScanner,
// but retrieves NSE cash-equity daily candles through Yahoo Finance ticker symbols.
func BuildYahooScanner(yh *Yahoo, names map[string]string, from, to time.Time, progress func(done, total int, sym string)) *ScanData {
	rows := []ScanRow{}
	var skipped []string
	for k, sym := range nifty50 {
		if progress != nil {
			progress(k, len(nifty50), sym)
		}
		ticker := sym + ".NS"
		cs, err := yh.Daily(ticker, from, to)
		if err != nil {
			skipped = append(skipped, sym+" ("+err.Error()+")")
			continue
		}
		cs = dropUnfinished(cs)
		if len(cs) < 251 {
			skipped = append(skipped, sym+" (short history)")
			continue
		}
		closes := make([]float64, len(cs))
		for i, c := range cs {
			closes[i] = c.C
		}
		r := pctReturns(closes)
		p, err := FitGARCH11(r)
		if err != nil {
			skipped = append(skipped, sym+" (GARCH fit failed)")
			continue
		}
		sg := Forecast1(r, p)
		cl := closes[len(closes)-1]
		l1, h1, l2, h2 := bands(cl, p.Mu, sg)
		name := names[sym]
		if name == "" {
			name = sym
		}
		rows = append(rows, ScanRow{Symbol: sym, Name: name, Close: cl, Sigma: sg, Lo1: l1, Hi1: h1, Lo2: l2, Hi2: h2,
			WidePc: (h2 - l2) / cl * 100, AsOf: cs[len(cs)-1].T.Format("2006-01-02"),
			Mu: p.Mu, Sig5: HorizonSigma(p, sg*sg, 5), Sig21: HorizonSigma(p, sg*sg, 21)})
		// Be polite to the unofficial public endpoint and avoid a burst of 50 requests.
		time.Sleep(150 * time.Millisecond)
	}
	sort.Slice(rows, func(a, b int) bool { return rows[a].WidePc > rows[b].WidePc })
	sd := &ScanData{Rows: rows, Skipped: skipped, Source: "live", FetchedAt: time.Now().Format(time.RFC3339)}
	if len(rows) > 0 {
		sd.AsOf = rows[0].AsOf
	}
	return sd
}

// YahooStock is a listed cash-equity instrument and its Yahoo Finance ticker.
type YahooStock struct {
	Symbol   string `json:"symbol"`
	Name     string `json:"name"`
	Ticker   string `json:"ticker"`
	Exchange string `json:"exchange"`
	ISIN     string `json:"isin,omitempty"`
}

// BuildYahooUniverseScanner fits the same GARCH model over a dynamically discovered
// NSE/BSE equity universe. A small worker pool reduces runtime while limiting bursts.
func BuildYahooUniverseScanner(yh *Yahoo, stocks []YahooStock, from, to time.Time, progress func(done, total int, sym string)) *ScanData {
	rows := make([]ScanRow, 0, len(stocks))
	skipped := make([]string, 0)
	var mu sync.Mutex
	var done int
	workers := 6
	if len(stocks) < workers {
		workers = len(stocks)
	}
	jobs := make(chan YahooStock)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for stock := range jobs {
				cs, err := yh.Daily(stock.Ticker, from, to)
				if err == nil {
					cs = dropUnfinished(cs)
					if len(cs) < 251 {
						err = fmt.Errorf("short history (%d candles)", len(cs))
					}
				}
				if err == nil {
					closes := make([]float64, len(cs))
					for i, c := range cs {
						closes[i] = c.C
					}
					r := pctReturns(closes)
					par, fitErr := FitGARCH11(r)
					if fitErr != nil {
						err = fmt.Errorf("GARCH fit failed")
					} else {
						sg := Forecast1(r, par)
						cl := closes[len(closes)-1]
						l1, h1, l2, h2 := bands(cl, par.Mu, sg)
						mu.Lock()
						rows = append(rows, ScanRow{Symbol: stock.Symbol, Name: stock.Name, Close: cl, Sigma: sg, Lo1: l1, Hi1: h1, Lo2: l2, Hi2: h2, WidePc: (h2 - l2) / cl * 100, AsOf: cs[len(cs)-1].T.Format("2006-01-02"), Mu: par.Mu, Sig5: HorizonSigma(par, sg*sg, 5), Sig21: HorizonSigma(par, sg*sg, 21)})
						mu.Unlock()
					}
				}
				mu.Lock()
				if err != nil {
					skipped = append(skipped, stock.Exchange+":"+stock.Symbol+" ("+err.Error()+")")
				}
				done++
				n := done
				mu.Unlock()
				if progress != nil {
					progress(n, len(stocks), stock.Exchange+":"+stock.Symbol)
				}
				// Keep a little spacing even with concurrent workers; Yahoo is unofficial and can rate-limit.
				time.Sleep(100 * time.Millisecond)
			}
		}()
	}
	for _, stock := range stocks {
		jobs <- stock
	}
	close(jobs)
	wg.Wait()
	sort.Slice(rows, func(a, b int) bool { return rows[a].WidePc > rows[b].WidePc })
	sd := &ScanData{Rows: rows, Skipped: skipped, Source: "live", FetchedAt: time.Now().Format(time.RFC3339)}
	if len(rows) > 0 {
		sd.AsOf = rows[0].AsOf
	}
	return sd
}
