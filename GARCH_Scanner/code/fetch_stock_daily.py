"""
Pull DAILY candles for the 50 Nifty 50 stocks from Dhan  ->  data/daily/<SYMBOL>.csv  and
data/universe.json. export_scanner.py then fits GARCH on each one.

Needs dhan_token.txt and dhan_client_id.txt next to this script (see fetch_daily.py).
Dhan's instrument list (https://images.dhan.co/api-data/api-scrip-master.csv) maps each stock's
trading symbol to its security id. Calls are paced to stay inside Dhan's data rate limit.
"""
import csv, datetime as dt, io, json, os, time, urllib.request
from export_scanner import NIFTY50

HERE = os.path.dirname(os.path.abspath(__file__))
IST = dt.timezone(dt.timedelta(hours=5, minutes=30))
OUT = os.path.join(HERE, "data", "daily")


def security_ids():
    raw = urllib.request.urlopen("https://images.dhan.co/api-data/api-scrip-master.csv", timeout=120).read().decode("utf-8", "ignore")
    rd = csv.DictReader(io.StringIO(raw))
    ids, names = {}, {}
    for r in rd:
        if r.get("SEM_EXM_EXCH_ID") == "NSE" and r.get("SEM_SEGMENT") == "E" and r.get("SEM_INSTRUMENT_NAME") == "EQUITY" and r.get("SEM_SERIES") == "EQ":
            s = r.get("SEM_TRADING_SYMBOL", "").strip().upper()
            if s in NIFTY50:
                ids[s] = r["SEM_SMST_SECURITY_ID"].strip()
                names[s] = (r.get("SM_SYMBOL_NAME") or s).strip().title()
    return ids, names


def candles(tok, client, sid, frm, to):
    body = json.dumps({"securityId": sid, "exchangeSegment": "NSE_EQ", "instrument": "EQUITY", "expiryCode": 0, "oi": False,
                       "fromDate": frm.strftime("%Y-%m-%d"), "toDate": to.strftime("%Y-%m-%d")}).encode()
    req = urllib.request.Request("https://api.dhan.co/v2/charts/historical", data=body,
                                 headers={"access-token": tok, "client-id": client, "Content-Type": "application/json", "Accept": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=90).read())


def main():
    tok = open(os.path.join(HERE, "dhan_token.txt")).read().strip()
    client = open(os.path.join(HERE, "dhan_client_id.txt")).read().strip()
    os.makedirs(OUT, exist_ok=True)
    ids, names = security_ids()
    now = dt.datetime.now(IST)
    for sym in NIFTY50:
        if sym not in ids:
            print("not in Dhan's list:", sym); continue
        rows = {}
        for a, b in ((now - dt.timedelta(days=5 * 365), now - dt.timedelta(days=2 * 365)), (now - dt.timedelta(days=2 * 365), now + dt.timedelta(days=1))):
            j = candles(tok, client, ids[sym], a, b); time.sleep(0.25)
            for t, o, h, l, c, v in zip(j.get("timestamp", []), j.get("open", []), j.get("high", []), j.get("low", []), j.get("close", []), j.get("volume", [])):
                d = dt.datetime.fromtimestamp(t, IST).date().isoformat()
                if c > 0:
                    rows[d] = (o, h, l, c, v)
        today = now.date().isoformat()
        if today in rows and now.hour * 60 + now.minute < 15 * 60 + 45:
            del rows[today]
        with open(os.path.join(OUT, sym + ".csv"), "w", newline="") as f:
            w = csv.writer(f); w.writerow(["date", "open", "high", "low", "close", "volume"])
            for d in sorted(rows):
                w.writerow([d, *rows[d]])
        print(sym, len(rows), "days")
    json.dump({"stocks": [{"symbol": s, "name": names.get(s, s)} for s in NIFTY50]}, open(os.path.join(HERE, "data", "universe.json"), "w"), indent=1)


if __name__ == "__main__":
    main()
