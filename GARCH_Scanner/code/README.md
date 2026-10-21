# Research scripts and datasets

This folder contains the original Python research notebooks/scripts and example datasets that shipped with the project. The modified Windows application uses the Go source in `../app_source` and fetches its data from Yahoo Finance without Dhan credentials.

The older `fetch_daily.py`, `fetch_intraday_5min.py`, and `fetch_stock_daily.py` scripts are retained as historical/reference scripts and may still use Dhan credentials; they are not called by the modified app. See the project `README.md` for Yahoo Finance build steps and limitations.
