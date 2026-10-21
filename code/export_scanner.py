"""
The scanner: today's GARCH(1,1) fence for every NIFTY 50 stock, one row each - same textbook
model as garch_nifty.py (constant mean, GARCH(1,1), normal distribution, fit by maximum
likelihood), just run once per stock instead of once for the index.

Data: run fetch_stock_daily.py first. It downloads each Nifty 50 stock's daily candles from Dhan
into data/daily/<SYMBOL>.csv and writes data/universe.json (symbol -> name).

Output: scanner_data.json - one row per stock: last close, tomorrow's sigma forecast, and the
inner (68%) / wide (95%) fence around tomorrow's close. Sorted by widest expected move first,
since a scanner's whole point is "which names are moving the most".
"""
import json, os, numpy as np, pandas as pd
from arch import arch_model

HERE = os.path.dirname(os.path.abspath(__file__))
DAILY_DIR = os.path.join(HERE, "data", "daily")
UNIVERSE = os.path.join(HERE, "data", "universe.json")

# the 50 NIFTY 50 index members (as of 23 Sep 2026). Update this list when the index changes.
NIFTY50 = ["ADANIENT","ADANIPORTS","APOLLOHOSP","ASIANPAINT","AXISBANK","BAJAJ-AUTO","BAJFINANCE",
"BAJAJFINSV","BEL","BHARTIARTL","CIPLA","COALINDIA","DRREDDY","EICHERMOT","ETERNAL","GRASIM",
"HCLTECH","HDFCBANK","HDFCLIFE","HINDALCO","HINDUNILVR","ICICIBANK","ITC","INFY","INDIGO",
"JSWSTEEL","JIOFIN","KOTAKBANK","LT","M&M","MARUTI","MAXHEALTH","NTPC","NESTLEIND","ONGC",
"POWERGRID","RELIANCE","SBILIFE","SHRIRAMFIN","SBIN","SUNPHARMA","TCS","TATACONSUM","TMPV",
"TATASTEEL","TECHM","TITAN","TRENT","ULTRACEMCO","WIPRO"]


def fit_one(symbol, name):
    path = os.path.join(DAILY_DIR, symbol + ".csv")
    if not os.path.exists(path):
        return None
    d = pd.read_csv(path, parse_dates=["date"]).set_index("date")
    r = (100 * np.log(d.close / d.close.shift(1))).dropna()
    if len(r) < 250:
        return None
    res = arch_model(r, mean="Constant", vol="GARCH", p=1, q=1, dist="normal").fit(disp="off")
    p = res.params
    mu, omega, alpha, beta = p["mu"], p["omega"], p["alpha[1]"], p["beta[1]"]
    sig1 = float(np.sqrt(res.forecast(horizon=1).variance.iloc[-1, 0]))
    close = float(d.close.iloc[-1])
    lo1 = close * np.exp((mu - sig1) / 100); hi1 = close * np.exp((mu + sig1) / 100)
    lo2 = close * np.exp((mu - 1.96 * sig1) / 100); hi2 = close * np.exp((mu + 1.96 * sig1) / 100)
    return {"symbol": symbol, "name": name, "close": close, "sigma_pct": sig1,
            "lo1": lo1, "hi1": hi1, "lo2": lo2, "hi2": hi2,
            "wide_pct": (hi2 - lo2) / close * 100, "as_of": str(d.index[-1].date())}


def main():
    u = json.load(open(UNIVERSE))
    names = {s["symbol"]: s["name"] for s in u["stocks"]}
    rows, failed = [], []
    for sym in NIFTY50:
        try:
            row = fit_one(sym, names.get(sym, sym))
            (rows if row else failed).append(row or sym)
        except Exception as e:
            failed.append(f"{sym}: {e}")
    rows.sort(key=lambda r: -r["wide_pct"])
    as_of = rows[0]["as_of"] if rows else None
    out = {"as_of": as_of, "rows": rows}
    json.dump(out, open(os.path.join(HERE, "scanner_data.json"), "w"), separators=(",", ":"))
    print(f"{len(rows)} of {len(NIFTY50)} fitted, as of {as_of}")
    if failed:
        print("failed/skipped:", failed)
    print("widest 5:", [(r["symbol"], round(r["wide_pct"], 2)) for r in rows[:5]])
    print("quietest 5:", [(r["symbol"], round(r["wide_pct"], 2)) for r in rows[-5:]])


if __name__ == "__main__":
    main()
