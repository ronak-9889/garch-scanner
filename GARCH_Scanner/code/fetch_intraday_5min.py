"""
Pull NIFTY 50 index 5-minute candles for the last 365 calendar days from Dhan.

Dhan's intraday endpoint (POST /v2/charts/historical intraday) only serves 90 days per
request, but the data itself does go back at least a year, so we page through it in
~80-day windows and stitch the pieces together.

The GARCH fence itself still comes only from daily closes (garch_nifty.py) - this file
only supplies the intraday PICTURE for the replay page, for the days it covers.

Source: DhanHQ API. Your access token lives in dhan_token.txt and your client id in
dhan_client_id.txt, both next to this script (your own; never share them). Neither is printed.

Output: nifty_5min.csv   date,time_min,open,high,low,close   (time_min = minutes since 09:15)
"""
import csv
import datetime as dt
import json
import os
import time
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
TOKEN_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "dhan_token.txt")
CLIENT_FILE = os.path.join(HERE, "dhan_client_id.txt")
SECURITY_ID = "13"           # NIFTY 50 index
DAYS_BACK = 365
CHUNK_DAYS = 80               # stay under Dhan's 90-day-per-call limit


def fetch_chunk(tok, client, frm, to):
    body = json.dumps({"securityId": SECURITY_ID, "exchangeSegment": "IDX_I", "instrument": "INDEX",
                        "interval": "5", "oi": False,
                        "fromDate": frm.strftime("%Y-%m-%d %H:%M:%S"),
                        "toDate": to.strftime("%Y-%m-%d %H:%M:%S")}).encode()
    req = urllib.request.Request(
        "https://api.dhan.co/v2/charts/intraday", data=body,
        headers={"access-token": tok, "client-id": client,
                 "Content-Type": "application/json", "Accept": "application/json"})
    r = urllib.request.urlopen(req, timeout=60)
    return json.loads(r.read())


def main():
    tok = open(TOKEN_FILE).read().strip()
    client = open(CLIENT_FILE).read().strip()
    IST = dt.timezone(dt.timedelta(hours=5, minutes=30))
    end = dt.datetime.now(IST)
    start = end - dt.timedelta(days=DAYS_BACK)

    rows = []   # (date_str, time_min, o, h, l, c)
    cur = start
    while cur < end:
        chunk_to = min(cur + dt.timedelta(days=CHUNK_DAYS), end)
        for attempt in range(3):
            try:
                j = fetch_chunk(tok, client, cur, chunk_to)
                break
            except urllib.error.HTTPError as e:
                print("HTTP", e.code, e.read()[:200]); time.sleep(3); j = None
            except Exception as e:
                print("ERR", repr(e)[:200]); time.sleep(3); j = None
        if j and "timestamp" in j:
            for t, o, h, l, c in zip(j["timestamp"], j["open"], j["high"], j["low"], j["close"]):
                d = dt.datetime.fromtimestamp(t, IST)
                mo = int((d - d.replace(hour=9, minute=15, second=0, microsecond=0)).total_seconds() // 60)
                if 0 <= mo <= 375:
                    rows.append((d.date().isoformat(), mo, o, h, l, c))
        print(cur.date(), "->", chunk_to.date(), ":", len(j["timestamp"]) if j and "timestamp" in j else 0, "bars")
        cur = chunk_to
        time.sleep(0.5)

    rows.sort(key=lambda r: (r[0], r[1]))
    # de-dupe (chunk boundaries can overlap by one bar)
    seen = set(); out = []
    for r in rows:
        k = (r[0], r[1])
        if k in seen: continue
        seen.add(k); out.append(r)

    with open(os.path.join(HERE, "nifty_5min.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["date", "time_min", "open", "high", "low", "close"])
        w.writerows(out)
    days = len(set(r[0] for r in out))
    print(f"\n{len(out)} bars across {days} trading days -> nifty_5min.csv")


if __name__ == "__main__":
    main()
