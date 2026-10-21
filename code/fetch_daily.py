"""
Pull NIFTY 50 index DAILY candles for the last 6 years from Dhan  ->  nifty50_daily.csv
(date,open,high,low,close,volume). This is the file garch_nifty.py fits GARCH on.

Needs, next to this script (your own; never share them, never commit them):
    dhan_token.txt       your Dhan access token
    dhan_client_id.txt   your Dhan client id
Today's candle is dropped until the market has closed, so the model only sees finished days.
"""
import csv, datetime as dt, json, os, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
IST = dt.timezone(dt.timedelta(hours=5, minutes=30))


def call(tok, client, frm, to):
    body = json.dumps({"securityId": "13", "exchangeSegment": "IDX_I", "instrument": "INDEX", "expiryCode": 0,
                       "oi": False, "fromDate": frm.strftime("%Y-%m-%d"), "toDate": to.strftime("%Y-%m-%d")}).encode()
    req = urllib.request.Request("https://api.dhan.co/v2/charts/historical", data=body,
                                 headers={"access-token": tok, "client-id": client,
                                          "Content-Type": "application/json", "Accept": "application/json"})
    return json.loads(urllib.request.urlopen(req, timeout=90).read())


def main():
    tok = open(os.path.join(HERE, "dhan_token.txt")).read().strip()
    client = open(os.path.join(HERE, "dhan_client_id.txt")).read().strip()
    now = dt.datetime.now(IST)
    rows, cur = {}, now - dt.timedelta(days=6 * 365)
    while cur < now:
        end = min(cur + dt.timedelta(days=3 * 365), now + dt.timedelta(days=1))
        j = call(tok, client, cur, end)
        for t, o, h, l, c, v in zip(j.get("timestamp", []), j["open"], j["high"], j["low"], j["close"], j["volume"]):
            d = dt.datetime.fromtimestamp(t, IST)
            if c > 0:
                rows[d.date().isoformat()] = (o, h, l, c, v)
        cur = end
    today = now.date().isoformat()
    if today in rows and now.hour * 60 + now.minute < 15 * 60 + 45:
        del rows[today]
    with open(os.path.join(HERE, "nifty50_daily.csv"), "w", newline="") as f:
        w = csv.writer(f)
        w.writerow(["date", "open", "high", "low", "close", "volume"])
        for d in sorted(rows):
            w.writerow([d, *rows[d]])
    print(f"{len(rows)} daily candles, {min(rows)} to {max(rows)} -> nifty50_daily.csv")


if __name__ == "__main__":
    main()
