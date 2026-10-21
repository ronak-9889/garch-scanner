# Predicting Nifty Prices (GARCH) - complete pack

    software/     the finished app. Double-click garch-volatility-windows-amd64.exe on Windows
                  (Mac: double-click Start-on-Mac.command, see "On a Mac" below; Linux: -linux-amd64)
    app_source/   the app's Go source (zero dependencies). build.bat / build.sh rebuild every file in software/
    code/         the Python research pipeline and the data it used (see code/README.md)
    PROMPT.docx, DESCRIPTION.docx, READING_LIST.docx

## Using the app
1. Double-click the .exe. A console window opens (leave it open) and your browser opens the app.
   Windows SmartScreen may warn about an unsigned app: More info > Run anyway.
2. First run: accept the agreement, then enter your Dhan client id and access token
   (Dhan's API page). They are saved only on this computer and sent only to api.dhan.co.
   "Look around with the bundled sample data" works without keys.
3. Every time you open it, the app pulls the latest Nifty daily and 5-minute candles and the 50 stocks,
   refits GARCH, and re-runs the 3-year honest test, so the chart, replay and scanner end on the latest close.
   Dhan access tokens expire; paste a fresh one (gear icon) when it stops working.
4. Chart + replay (page 1) - what is GARCH / why it works / how it was made (header buttons) - Scanner (page 2).

It only reads candles. It never places an order. Educational only; not investment advice.
Close the console window to stop it. Saved settings: %AppData%\GarchVolatility (Windows).

## Checks run
Go GARCH vs the Python `arch` reference on the pack's data: parameters agree to 3-4 digits, per-day sigma within 0.002 percentage points,
wide fence held 718 of 756 days in both. `go test` runs the whole pipeline against a fake Dhan server built from the pack's CSVs.


## On a Mac
A Mac will not open the downloaded app by a plain double-click (it is unsigned). Use the launcher:
1. Unzip the pack, open the software folder.
2. Right-click Start-on-Mac.command > Open > Open (needed only the first time; if it says it cannot be opened, go to
   System Settings > Privacy & Security > Open Anyway).
3. A Terminal window opens and your browser opens the app. Leave Terminal open; close it to stop.
It picks the Apple-chip or Intel file by itself. Manual way, in Terminal (replace -arm64 with -intel on Intel Macs):
    cd path/to/software
    chmod +x garch-volatility-mac-arm64
    xattr -d com.apple.quarantine garch-volatility-mac-arm64
    ./garch-volatility-mac-arm64
Your saved keys and data are in ~/Library/Application Support/GarchVolatility.


## Yahoo Finance prototype build (modified edition)

This modified source replaces the app's Dhan-dependent refresh workflow with Yahoo Finance chart-endpoint requests. It preserves the existing Go GARCH(1,1) calculations and replay interface, and expands the scanner to discover NSE EQ-series stocks plus BSE active-equity scrips. No Dhan account, client ID, or access token is required.

### Build on Windows

1. Install Go 1.22 or later from https://go.dev/dl/.
2. Extract the ZIP to a normal folder (avoid running it from inside the ZIP).
3. Open `app_source` in Command Prompt.
4. Run `build.bat`.
5. Launch `software\garch-volatility-windows-amd64.exe` from the extracted project folder.
6. Accept the agreement, wait for the refresh, then use the chart and scanner. Click the gear/settings icon to refresh or review source status.

### Yahoo Finance limitations

- This uses Yahoo Finance's unofficial chart endpoint, not a guaranteed or licensed real-time exchange feed. Requests may be delayed, throttled, blocked, or changed by Yahoo without notice.
- Daily candles are requested for six years for Nifty. The all-market scanner requests three years for each discovered NSE/BSE equity; the first scan can take a long time and Yahoo may throttle or block some requests. A successful full scan is reused for the rest of the same local day. Availability varies by symbol. Missing or short histories are reported as skipped stocks.
- Intraday 5-minute data is requested only for about the most recent 59 days because Yahoo generally limits intraday history to around 60 days. The replay therefore will not have a full year of intraday bars. If the intraday request fails, the daily GARCH forecast can still work but intraday replay bars may be absent.
- The Nifty index ticker is `^NSEI`; NSE symbols use Yahoo's `.NS` suffix, while BSE-only entries are tried by numeric scrip code with `.BO`. Official exchange lists are refreshed daily and cached locally. If the BSE list endpoint is unavailable, the app warns and scans the NSE list it can retrieve. Data can differ from exchange or broker feeds and must be validated before any research use.
- The model forecasts volatility/range, not direction or guaranteed highs/lows. This software never places orders.


## Modified Yahoo Finance edition

The supplied modified Windows build and `app_source` use Yahoo Finance instead of requiring Dhan credentials. See `app_source/README.md` for the build instructions and important data-source limitations. Only `software/garch-volatility-windows-amd64.exe` is rebuilt for this edition; other platform binaries from the original archive are not included to avoid accidentally running the old Dhan version.
Global Markets fix (2026-10-21): corrected the percentage-change field mapping so daily changes display as numeric percentages instead of `NaN%`; added defensive validation for invalid close values.
