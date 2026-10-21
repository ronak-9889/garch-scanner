"""Build replay_data.json for the replay page: three years of Nifty candles with the out-of-sample
GARCH forecast for each day (from garch_series.csv), the hit/miss per day, and tomorrow's cone.

Every historic fence uses the SAME out-of-sample mu that scored it (mu_oos_pct in garch_series.csv,
refit every 21 days on data up to the day before) - not the full-sample mu, which would be a small
look-ahead. Tomorrow's fence is built the identical way, from garch_results.json (already fixed
there to use the same mu-centred formula), so the chart never switches rules between "history" and
"tomorrow". The GARCH fence is always a whole-day number either way - only the picture changes.

For the most recent 365 calendar days, Dhan also gives us real 5-minute NIFTY candles
(fetch_intraday_5min.py). For those days we attach the intraday bars under "m5" so the replay
page can draw the actual intraday path inside that day's fence, instead of one daily bar landing
in a single step. Older days simply have no "m5" and render as a single daily candle, as before."""
import csv, json, os, numpy as np, pandas as pd
HERE = os.path.dirname(os.path.abspath(__file__))
LEAD = 40   # candles shown before the first forecast, so the chart is not empty at the start

d = pd.read_csv(os.path.join(HERE, "nifty50_daily.csv"), parse_dates=["date"])
s = pd.read_csv(os.path.join(HERE, "garch_series.csv"), parse_dates=["date"])
R = json.load(open(os.path.join(HERE, "garch_results.json")))
sig = dict(zip(s.date, s.garch_sigma_pct))
mu_by_date = dict(zip(s.date, s.mu_oos_pct))
first = s.date.min()
d = d[d.date >= d.date[d.date < first].iloc[-LEAD:].min()].reset_index(drop=True)

# intraday 5-min bars, grouped by date - only exists for the last ~365 days
m5_by_date, m5_path = {}, os.path.join(HERE, "nifty_5min.csv")
if os.path.exists(m5_path):
    with open(m5_path, newline="") as f:
        for row in csv.DictReader(f):
            m5_by_date.setdefault(row["date"], []).append(
                [int(row["time_min"]), round(float(row["open"]), 2), round(float(row["high"]), 2),
                 round(float(row["low"]), 2), round(float(row["close"]), 2)])
    for v in m5_by_date.values(): v.sort(key=lambda b: b[0])
intraday_from = min(m5_by_date) if m5_by_date else None

days = []
for i, r in d.iterrows():
    row = {"d": r.date.strftime("%Y-%m-%d"), "o": r.open, "h": r.high, "l": r.low, "c": r.close}
    if r.date in sig and i > 0:
        pc = d.close.iloc[i - 1]
        sg, mu_t = sig[r.date], mu_by_date[r.date]
        ret = 100 * np.log(r.close / pc)
        dev = abs(ret - mu_t)
        row.update({"s": round(sg, 4), "ret": round(ret, 3), "mu": round(mu_t, 4),
                    "in1": bool(dev <= sg), "in2": bool(dev <= 1.96 * sg),
                    "lo1": pc * np.exp((mu_t - sg) / 100), "hi1": pc * np.exp((mu_t + sg) / 100),
                    "lo2": pc * np.exp((mu_t - 1.96 * sg) / 100), "hi2": pc * np.exp((mu_t + 1.96 * sg) / 100)})
    if row["d"] in m5_by_date:
        row["m5"] = m5_by_date[row["d"]]
    days.append(row)

# captions for the biggest breaks.  Only the three we can name for certain; everything else stays
# unlabelled rather than guessing a cause the data cannot support.
LABELS = {"2024-06-04": "Election result day", "2025-04-07": "US tariff shock", "2025-05-12": "Ceasefire rally"}
breaks = [{"d": x["d"], "ret": x["ret"], "s": x["s"], "label": LABELS.get(x["d"], "")} for x in days if "in2" in x and not x["in2"]]
breaks_by_size = sorted(breaks, key=lambda b: -abs(b["ret"]))
biggest = breaks_by_size[0] if breaks_by_size else None
named_biggest = next((b for b in breaks_by_size if b["label"]), None)

f = R["forecast"]
lo1, hi1 = f["bands_next_close"]["68pct"]
lo2, hi2 = f["bands_next_close"]["95pct"]
close = R["last_close"]
tomorrow = {"sigma": f["sigma_1d_pct"], "close": close, "from": R["data_to"],
            "lo1": lo1, "hi1": hi1, "lo2": lo2, "hi2": hi2,
            # signed half-widths in points, since the mu-shift makes the band slightly asymmetric
            "dn1": close - lo1, "up1": hi1 - close, "dn2": close - lo2, "up2": hi2 - close}

out = {"days": days, "tomorrow": tomorrow, "params": R["params"], "check": R["check_last_3y"], "breaks": breaks,
       "biggest_break": biggest, "named_biggest_break": named_biggest,
       "data_from": R["data_from"], "data_to": R["data_to"], "intraday_from": intraday_from,
       "totals": {"n": sum(1 for x in days if "in2" in x), "in1": sum(1 for x in days if x.get("in1")), "in2": sum(1 for x in days if x.get("in2"))}}
json.dump(out, open(os.path.join(HERE, "replay_data.json"), "w"), separators=(",", ":"))
print(len(days), "days,", out["totals"], "| breaks", len(breaks), "| biggest", biggest,
      "| tomorrow", {k: round(v, 1) for k, v in tomorrow.items() if isinstance(v, float)},
      "| intraday days", sum(1 for x in days if "m5" in x), "from", intraday_from)
