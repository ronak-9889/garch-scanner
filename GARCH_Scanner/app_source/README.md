# GARCH Scanner

## Current scope
- GARCH(1,1) Nifty 50 benchmark chart and volatility forecast.
- Full discovered NSE/BSE equity-universe scanner using Yahoo Finance data where available; not restricted to Nifty 50 constituents. Coverage is best-effort and depends on data availability.
- Separate global benchmark monitor for Taiwan Weighted Index (`^TWII`), Nasdaq Composite (`^IXIC`), S&P 500 (`^GSPC`) and KOSPI (`^KS11`). It monitors indices only, not individual US stocks or ETFs.
- Yahoo Finance is the default data provider.
- Optional Dhan provider setting for users who have Dhan API access: Dhan supplies Nifty 50 benchmark daily and intraday candles. The full NSE/BSE scanner and global indices remain on Yahoo Finance in this release. Dhan Data API subscription/entitlements may be required.

## Configure provider
1. Start the application and accept the agreement.
2. Open `http://localhost:8766/setup?edit=1`.
3. Choose Yahoo Finance or Dhan API. For Dhan, enter your own Client ID and access token, then save settings.
4. Select **Save and run refresh**.

Dhan credentials are stored locally in the user's local application configuration directory. Keep that file private. This software does not place orders.

## Build and run
- Requires Go 1.22 or later.
- Windows: run `app_source/build.bat` from the extracted project.
- Linux/macOS: run `bash app_source/build.sh`.
- The output executable is written to `software/`.

## Data and limitations
Yahoo Finance access is unofficial, best-effort and not a guaranteed real-time or licensed exchange feed. It can omit symbols, rate-limit requests or return unavailable index data. Dhan data access depends on the user's account, API entitlement and provider limits. The scanner only reports symbols for which usable historical candles and a GARCH fit were available; it does not guarantee coverage of every listed security. Global index data is displayed independently, so one unavailable index does not block the others.

Educational only; not investment advice. This software never places an order.
