# GARCH Scanner

GARCH Scanner is an educational market-analysis application that estimates volatility and statistical price ranges using a GARCH(1,1) model. It includes a broad NSE/BSE equity scanner and a Global Markets monitor for selected international indices.

## Features

- **GARCH range forecasts:** Estimates statistical price ranges rather than predicting whether a market will rise or fall. Forecasts are estimates, not guaranteed highs, lows, or trading signals.
- **NSE/BSE equity scanner:** Discovers available NSE EQ-series stocks and BSE active-equity scrips, subject to the availability and quality of the underlying data.
- **Global Markets monitor:** Tracks these benchmark indices:
  - Taiwan Weighted Index (TAIEX) — `^TWII`
  - Nasdaq Composite — `^IXIC`
  - S&P 500 — `^GSPC`
  - KOSPI — `^KS11`
- **Global range forecasts:** Estimates volatility-based ranges for each supported index across the forecast horizons shown in the application. Forecasts depend on sufficient historical data being available.
- **Selectable data provider:** Yahoo Finance is the default provider. Dhan can be selected as an optional provider for supported Indian benchmark data when the user supplies valid credentials and has the required API access.
- **Local application:** Runs on the user's computer and opens its interface in a web browser.
- **No order placement:** The application reads market data and performs analysis; it does not place trades.

## Download and run on Windows

1. Download and extract the Windows release ZIP into a normal folder.
2. Open the `software` folder.
3. Double-click `garch-volatility-windows-amd64.exe`.
4. Leave the console window open while using the application; close it to stop the app.
5. Open the local address displayed by the application if the browser does not open automatically.

Windows may display a security warning because the executable is unsigned. Only run software you have obtained from a source you trust.

## Data providers

### Yahoo Finance

Yahoo Finance is the default source for the global indices and the broad NSE/BSE equity scan. The application uses Yahoo Finance's unofficial chart endpoint.

- Data can be delayed, incomplete, throttled, unavailable for some symbols, or changed without notice.
- The first broad-market scan may take time. Some symbols may be skipped if there is insufficient history or a request fails.
- Intraday history is more limited than daily history, so intraday replay may not cover the same period as daily forecasts.
- Index ticker availability and returned history can vary. An unavailable or invalid result should not be interpreted as a forecast.

Yahoo Finance is not a guaranteed real-time exchange feed. Validate data before relying on it for research.

### Optional Dhan API

Users may choose Dhan in **Data Provider Settings** and enter their own Dhan Client ID and access token. Dhan access requires an eligible account and the appropriate API/data entitlement; fees and terms may apply. Tokens can expire and may need to be renewed.

In this release, Dhan is an optional provider for supported Indian benchmark data. The broad NSE/BSE equity scanner and global index monitor use Yahoo Finance; selecting Dhan does not mean Dhan supplies every stock or international index shown in the application.

Credentials should be entered only in the local application and must never be committed to a public repository or shared in screenshots, logs, or issue reports.

## Build from source

Requirements: Go 1.22 or later.

1. Extract the project ZIP.
2. Open the `app_source` directory in Command Prompt or PowerShell.
3. On Windows, run:

   ```bat
   build.bat
   ```

4. Launch `software\\garch-volatility-windows-amd64.exe`.

The source build scripts are included in `app_source`. Check that directory's README for any additional platform-specific details.

## Demo

Watch the GARCH Scanner demonstration:

[▶ Watch the demo video on YouTube](https://www.youtube.com/watch?v=uaikF12vdrA)

### Screenshots

![GARCH Scanner dashboard](screenshots/main-dashboard.png)

## Interpretation and limitations

GARCH Scanner estimates conditional volatility and statistical ranges from historical observations. It does **not** predict market direction, guarantee future prices, or provide investment advice. Real outcomes can fall outside the estimated ranges, particularly during market gaps or unusual events. Forecast quality depends on input-data quality, history length, model assumptions, and market conditions.

The application is for educational and research use only. It does not place orders. The user is responsible for checking data-provider terms, API entitlements, local regulations, and the suitability of any analysis.

## Credits

GARCH Scanner builds on the original GARCH-based Nifty range forecasting project shared on MadeForTrade. Sincere thanks to the original author for the foundational work and for granting permission to publish this extension.

Original discussion and project:
[Predict tomorrow's Nifty range with AI GARCH model](https://madefortrade.in/t/predict-tomorrow-s-nifty-range-with-ai-garch-model/94540)

Please retain appropriate attribution to the original author and comply with the original project's license and any applicable third-party terms when redistributing this project.
