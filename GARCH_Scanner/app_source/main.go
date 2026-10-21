// GARCH for Volatility Prediction - Nifty 50 benchmark plus the full discovered NSE/BSE equity universe, the textbook model (Bollerslev
// 1986), fit fresh on your own Dhan account's data every time you open it.
//
// Yahoo Finance is the default. Dhan can optionally supply the Nifty 50 benchmark candles;
// the full NSE/BSE stock scanner and global indices continue to use Yahoo Finance in this release.
package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

//go:embed assets/*.html assets/agreement.txt assets/sample/*.json
var assets embed.FS

const version = "1.5.0-provider-settings"
const defaultPort = 8766

type config struct {
	Agreed       string `json:"agreed_at"`
	AgreedVer    string `json:"agreed_version"`
	Provider     string `json:"provider"`
	DhanClientID string `json:"dhan_client_id"`
	DhanToken    string `json:"dhan_access_token"`
}

var (
	cfgDir string
	cfg    config

	mu        sync.Mutex
	status    = "idle" // idle | running | done | error
	stepMsg   string
	stepPct   float64
	lastErr   string
	replay    *ReplayData // current data: live, saved, or sample
	scanner   *ScanData
	markets   *MarketsData
	liveReady bool // a refresh finished in this session
	useSaved  bool // (kept for older callers; the chart now always opens on whatever data exists)
	demoMode  bool
	pending   bool // keys changed or refresh asked while a run was in progress: run again after
)

func loadCfg() {
	d, err := os.UserConfigDir()
	if err != nil || d == "" {
		d = os.TempDir()
	}
	cfgDir = filepath.Join(d, "GarchVolatility")
	os.MkdirAll(cfgDir, 0o700)
	if b, err := os.ReadFile(filepath.Join(cfgDir, "config.json")); err == nil {
		json.Unmarshal(b, &cfg)
	}
}
func saveCfg() {
	b, _ := json.MarshalIndent(cfg, "", " ")
	os.WriteFile(filepath.Join(cfgDir, "config.json"), b, 0o600)
}

func setStep(msg string, pct float64) {
	mu.Lock()
	stepMsg, stepPct = msg, pct
	mu.Unlock()
}
func fail(err error) {
	mu.Lock()
	status, lastErr = "error", err.Error()
	mu.Unlock()
}

// run: the full refresh. Only one runs at a time; if asked again meanwhile (for example you pasted
// a new token), it runs once more with the newest keys as soon as the current one stops.
func run() {
	mu.Lock()
	if status == "running" {
		pending = true
		mu.Unlock()
		return
	}
	status = "running"
	mu.Unlock()
	for {
		runOnce()
		mu.Lock()
		if !pending {
			mu.Unlock()
			return
		}
		pending, status = false, "running"
		mu.Unlock()
	}
}

func runOnce() {
	mu.Lock()
	status, lastErr, stepMsg, stepPct = "running", "", "Connecting to Yahoo Finance", 0
	mu.Unlock()
	yh := NewYahoo()
	now := time.Now().In(IST)
	to := now.AddDate(0, 0, 1)

	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider != "dhan" {
		provider = "yahoo"
	}
	var daily, intra []Candle
	var err error
	if provider == "dhan" {
		if strings.TrimSpace(cfg.DhanToken) == "" || strings.TrimSpace(cfg.DhanClientID) == "" {
			fail(fmt.Errorf("Dhan is selected but Client ID or Access Token is missing. Open /setup?edit=1 and enter your Dhan credentials, or switch back to Yahoo Finance."))
			return
		}
		setStep("Downloading Nifty 50 daily candles (6 years) from Dhan", 0.03)
		dh := NewDhan(strings.TrimSpace(cfg.DhanClientID), strings.TrimSpace(cfg.DhanToken))
		// Dhan instrument master identifies the Nifty 50 index as security ID 13 in IDX_I.
		daily, err = dh.Daily("13", "IDX_I", "INDEX", now.AddDate(-6, 0, 0), to)
		if err != nil {
			fail(err)
			return
		}
		setStep("Downloading recent 5-minute Nifty 50 candles from Dhan", 0.12)
		intra, err = dh.Intraday5("13", "IDX_I", "INDEX", now.AddDate(0, 0, -59), now, func(f float64) { setStep("Downloading recent 5-minute candles from Dhan", 0.12+0.2*f) })
	} else {
		setStep("Downloading Nifty 50 daily candles (6 years) from Yahoo Finance", 0.03)
		daily, err = yh.Daily("^NSEI", now.AddDate(-6, 0, 0), to)
		if err != nil {
			fail(err)
			return
		}
		setStep("Downloading recent 5-minute candles from Yahoo Finance (up to about 59 days)", 0.12)
		intra, err = yh.Intraday5("^NSEI", now.AddDate(0, 0, -59), now, func(f float64) { setStep("Downloading recent 5-minute candles from Yahoo Finance", 0.12+0.2*f) })
	}
	if err != nil { // daily candles still support the chart without intraday candles
		intra = nil
	}
	daily = dropUnfinished(daily)

	setStep("Updating global index monitor (Taiwan, Nasdaq Composite, S&P 500, KOSPI)", 0.32)
	md := BuildMarkets(yh, now, func(name string) { setStep("Global indices: "+name, 0.32) })
	if md != nil {
		saveJSON("markets.json", md)
		mu.Lock()
		markets = md
		mu.Unlock()
	}

	setStep("Fitting GARCH(1,1) and checking it on 3 years it had never seen", 0.35)
	rd, err := BuildReplay(daily, intra, now)
	if err != nil {
		fail(err)
		return
	}
	saveJSON("replay.json", rd)
	mu.Lock()
	replay = rd
	mu.Unlock()

	// A full-market scan can include thousands of tickers. Reuse today's successful
	// full scan when the app is reopened, while still refreshing Nifty's own forecast.
	if cached := loadScannerForToday(now); cached != nil {
		mu.Lock()
		scanner = cached
		status, liveReady, stepPct = "done", true, 1
		mu.Unlock()
		return
	}
	setStep("Loading the official NSE/BSE equity universe", 0.55)
	universePath := filepath.Join(cfgDir, "equity_universe.json")
	stocks, warnings, universeErr := FetchYahooUniverse(universePath)
	if universeErr != nil {
		fail(universeErr)
		return
	}
	setStep(fmt.Sprintf("Loaded %d NSE/BSE equity symbols; fitting GARCH forecasts", len(stocks)), 0.57)
	// Three years is enough to fit the model while keeping an all-market first scan manageable.
	sd := BuildYahooUniverseScanner(yh, stocks, now.AddDate(-3, 0, 0), to, func(k, n int, sym string) {
		pct := 0.58
		if n > 0 {
			pct += 0.4 * float64(k) / float64(n)
		}
		setStep(fmt.Sprintf("Yahoo scanner: %s (%d of %d)", sym, k, n), pct)
	})
	if sd != nil {
		sd.Skipped = append(warnings, sd.Skipped...)
	}
	if sd != nil && len(sd.Rows) > 0 {
		saveJSON("scanner.json", sd)
		mu.Lock()
		scanner = sd
		mu.Unlock()
	}
	mu.Lock()
	status, liveReady, stepPct = "done", true, 1
	if sd == nil || len(sd.Rows) == 0 {
		lastErr = "Nifty is up to date, but the stock scanner could not be refreshed."
		if err != nil {
			lastErr += " (" + err.Error() + ")"
		}
	}
	mu.Unlock()
}

func cachedSecurityIDs() (map[string]string, error) {
	p := filepath.Join(cfgDir, "security_ids.json")
	if fi, err := os.Stat(p); err == nil && time.Since(fi.ModTime()) < 7*24*time.Hour {
		var m map[string]string
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &m) == nil && len(m) >= 40 {
			return m, nil
		}
	}
	m, err := SecurityIDs(nifty50)
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, fmt.Errorf("none of the Nifty 50 symbols were found in Dhan's instrument list")
	}
	b, _ := json.Marshal(m)
	os.WriteFile(p, b, 0o600)
	return m, nil
}

func saveJSON(name string, v any) {
	b, _ := json.Marshal(v)
	os.WriteFile(filepath.Join(cfgDir, name), b, 0o600)
}

func loadSampleReplay() *ReplayData {
	b, err := assets.ReadFile("assets/sample/replay_data.json")
	if err != nil {
		return nil
	}
	var r ReplayData
	if json.Unmarshal(b, &r) != nil {
		return nil
	}
	r.Source = "sample"
	return &r
}
func loadScannerForToday(now time.Time) *ScanData {
	b, err := os.ReadFile(filepath.Join(cfgDir, "scanner.json"))
	if err != nil {
		return nil
	}
	var sd ScanData
	if json.Unmarshal(b, &sd) != nil || len(sd.Rows) < 100 {
		return nil
	}
	t, err := time.Parse(time.RFC3339, sd.FetchedAt)
	if err != nil || t.In(IST).Format("2006-01-02") != now.In(IST).Format("2006-01-02") {
		return nil
	}
	sd.Source = "live"
	return &sd
}

func loadSampleScanner() *ScanData {
	b, err := assets.ReadFile("assets/sample/scanner_data.json")
	if err != nil {
		return nil
	}
	var s ScanData
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	s.Source = "sample"
	return &s
}

// currentData: what the pages show - live if this session refreshed, else last saved, else the
// bundled sample (clearly labelled as such).
func currentData() (*ReplayData, *ScanData) {
	mu.Lock()
	r, s := replay, scanner
	mu.Unlock()
	if r == nil {
		if b, err := os.ReadFile(filepath.Join(cfgDir, "replay.json")); err == nil {
			var x ReplayData
			if json.Unmarshal(b, &x) == nil && len(x.Days) > 0 {
				x.Source = "saved"
				r = &x
			}
		}
	}
	if r == nil {
		r = loadSampleReplay()
	}
	if s == nil {
		if b, err := os.ReadFile(filepath.Join(cfgDir, "scanner.json")); err == nil {
			var x ScanData
			if json.Unmarshal(b, &x) == nil && len(x.Rows) > 0 {
				x.Source = "saved"
				s = &x
			}
		}
	}
	if s == nil {
		s = loadSampleScanner()
	}
	return r, s
}

func providerName() string {
	if strings.EqualFold(cfg.Provider, "dhan") {
		return "dhan"
	}
	return "yahoo"
}

func agreed() bool { return cfg.Agreed != "" && cfg.AgreedVer == version }

// ready: should "/" show the chart rather than the setup page? Yes as soon as the agreement is
// accepted and there are keys (or sample mode): the chart opens at once on the data already here
// (last saved run, else the bundled sample) while a refresh runs in the background.
func ready() bool {
	mu.Lock()
	defer mu.Unlock()
	return agreed()
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}

// guard: only this computer's own browser tab may talk to the app (blocks other websites from
// poking at it) and never cache.
func guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if i := strings.LastIndex(host, ":"); i >= 0 {
			host = host[:i]
		}
		if host != "localhost" && host != "127.0.0.1" {
			http.Error(w, "forbidden", 403)
			return
		}
		if r.Method == "POST" {
			if o := r.Header.Get("Origin"); o != "" && !strings.HasSuffix(o, "://"+r.Host) {
				http.Error(w, "forbidden", 403)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		h(w, r)
	}
}

func page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, err := assets.ReadFile("assets/" + name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(b)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func main() {
	if len(os.Args) > 2 && os.Args[1] == "-verify" {
		runVerify(os.Args[2])
		return
	}
	loadCfg()
	agreement, _ := assets.ReadFile("assets/agreement.txt")
	terminal, scan, setup, marketPage := page("terminal.html"), page("scanner.html"), page("setup.html"), page("markets.html")

	mux := http.NewServeMux()
	mux.HandleFunc("/", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		if !ready() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		terminal(w, r)
	}))
	mux.HandleFunc("/scanner", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		scan(w, r)
	}))
	mux.HandleFunc("/setup", guard(setup))
	mux.HandleFunc("/markets", guard(func(w http.ResponseWriter, r *http.Request) {
		if !ready() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		marketPage(w, r)
	}))
	mux.HandleFunc("/garch/markets/data", guard(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		md := markets
		mu.Unlock()
		if md == nil {
			if b, err := os.ReadFile(filepath.Join(cfgDir, "markets.json")); err == nil {
				var saved MarketsData
				if json.Unmarshal(b, &saved) == nil {
					saved.Source = "saved"
					md = &saved
				}
			}
		}
		if md == nil {
			http.Error(w, "global market data not available yet", 503)
			return
		}
		writeJSON(w, md)
	}))
	mux.HandleFunc("/api/state", guard(func(w http.ResponseWriter, r *http.Request) {
		rd, sd := currentData()
		mu.Lock()
		defer mu.Unlock()
		src, asof, scanAsof, fetched := "", "", "", ""
		if rd != nil {
			src, asof, fetched = rd.Source, rd.DataTo, rd.FetchedAt
		}
		if sd != nil {
			scanAsof = sd.AsOf
		}
		writeJSON(w, map[string]any{"version": version, "provider": providerName(), "dhan_configured": cfg.DhanClientID != "" && cfg.DhanToken != "", "agreed": agreed(), "agreement": string(agreement),
			"status": status, "step": stepMsg, "progress": stepPct, "error": lastErr, "markets": marketsSummary(markets), "ready": agreed(), "refreshing": status == "running", "source": src, "as_of": asof, "fetched_at": fetched, "scanner_as_of": scanAsof, "has_saved": rd != nil, "config_dir": cfgDir})
	}))
	mux.HandleFunc("/api/provider", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "method not allowed", 405)
			return
		}
		var v struct {
			Provider string `json:"provider"`
			ClientID string `json:"client_id"`
			Token    string `json:"token"`
		}
		if json.NewDecoder(r.Body).Decode(&v) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		v.Provider = strings.ToLower(strings.TrimSpace(v.Provider))
		if v.Provider != "yahoo" && v.Provider != "dhan" {
			http.Error(w, "provider must be yahoo or dhan", 400)
			return
		}
		if v.Provider == "dhan" && (strings.TrimSpace(v.ClientID) == "" || strings.TrimSpace(v.Token) == "") {
			http.Error(w, "Dhan Client ID and Access Token are both required", 400)
			return
		}
		cfg.Provider = v.Provider
		if v.ClientID != "" {
			cfg.DhanClientID = strings.TrimSpace(v.ClientID)
		}
		if v.Token != "" {
			cfg.DhanToken = strings.TrimSpace(v.Token)
		}
		if cfg.Provider == "dhan" && (cfg.DhanClientID == "" || cfg.DhanToken == "") {
			http.Error(w, "Dhan credentials are not configured", 400)
			return
		}
		saveCfg()
		writeJSON(w, map[string]any{"ok": true, "provider": providerName()})
	}))
	mux.HandleFunc("/api/agree", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			return
		}
		cfg.Agreed, cfg.AgreedVer = time.Now().Format(time.RFC3339), version
		saveCfg()
		if !liveReady {
			go run()
		}
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/api/run", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			return
		}
		mu.Lock()
		liveReady, useSaved = false, false
		mu.Unlock()
		go run()
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/api/demo", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			return
		}
		mu.Lock()
		demoMode, useSaved = true, true
		mu.Unlock()
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/api/usesaved", guard(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			return
		}
		mu.Lock()
		useSaved = true
		mu.Unlock()
		w.Write([]byte("ok"))
	}))
	mux.HandleFunc("/garch/replay/data", guard(func(w http.ResponseWriter, r *http.Request) {
		rd, _ := currentData()
		if rd == nil {
			http.Error(w, "no data yet", 503)
			return
		}
		writeJSON(w, rd)
	}))
	mux.HandleFunc("/garch/horizons", guard(func(w http.ResponseWriter, r *http.Request) {
		rd, _ := currentData()
		if rd == nil {
			http.Error(w, "no data yet", 503)
			return
		}
		hs := rd.Horizons
		if len(hs) == 0 && rd.Params.Omega > 0 { // sample / older saved data: fences from the fitted parameters
			hs = makeHorizons(Params{rd.Params.Mu, rd.Params.Omega, rd.Params.Alpha, rd.Params.Beta}, rd.Tomorrow.Close, rd.Tomorrow.Sigma)
		}
		writeJSON(w, map[string]any{"as_of": rd.DataTo, "close": rd.Tomorrow.Close, "source": rd.Source, "horizons": hs})
	}))
	mux.HandleFunc("/garch/scanner/data", guard(func(w http.ResponseWriter, r *http.Request) {
		_, sd := currentData()
		if sd == nil {
			http.Error(w, "no data yet", 503)
			return
		}
		writeJSON(w, sd)
	}))

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", defaultPort))
	port := defaultPort
	if err != nil {
		// already running? then just open that one
		if resp, e := (&http.Client{Timeout: 2 * time.Second}).Get(fmt.Sprintf("http://localhost:%d/api/state", defaultPort)); e == nil {
			resp.Body.Close()
			fmt.Println("GARCH for Volatility Prediction is already running - opening it.")
			openBrowser(fmt.Sprintf("http://localhost:%d", defaultPort))
			return
		}
		ln, err = net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Println("cannot start:", err)
			os.Exit(1)
		}
		port = ln.Addr().(*net.TCPAddr).Port
	}
	url := fmt.Sprintf("http://localhost:%d", port)
	fmt.Println("GARCH for Volatility Prediction", version, "(Yahoo Finance default; Dhan optional for Nifty 50 benchmark candles)")
	fmt.Println("Open:", url, "   (a browser tab opens by itself; close this window to stop)")
	fmt.Println("Educational only. Not investment advice. This software never places an order.")
	if agreed() {
		fmt.Println("Refreshing from Yahoo Finance...")
		go run()
	}
	go func() { time.Sleep(400 * time.Millisecond); openBrowser(url) }()
	_ = math.Pi
	if err := http.Serve(ln, mux); err != nil {
		fmt.Println(err)
	}
}
