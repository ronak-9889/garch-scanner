"""
GARCH(1,1) on Nifty 50 - the textbook version, used the textbook way.

THE MODEL (Bollerslev 1986, built on Engle 1982)
    r_t      = mu + e_t                 daily return = a constant mean + a surprise
    e_t      = sigma_t * z_t            the surprise is scaled by today's volatility, z ~ N(0,1)
    sigma_t^2 = omega + alpha * e_{t-1}^2 + beta * sigma_{t-1}^2
                 long-run   yesterday's     yesterday's
                 level      surprise        volatility
    Fitted by maximum likelihood.  Standard reads:
        alpha + beta          persistence: how slowly volatility fades (near 1 = slowly)
        half-life             ln(0.5) / ln(alpha + beta), in days
        unconditional sigma   sqrt(omega / (1 - alpha - beta)): the long-run daily volatility

THE DATA   daily close-to-close returns of Nifty 50, in percent, r_t = 100 * ln(C_t / C_{t-1})

THE USES (what a textbook does with the fitted model)
    1. one-step forecast   sigma_{t+1}: the expected size of tomorrow's move
    2. bands               tomorrow's close within +/- 1 sigma on ~68% of days, +/- 1.96 sigma on 95%
    3. VaR                 the 95% and 99% one-day loss on a long position
    4. term structure      the forecast for 1, 5, 10, 21 days ahead (it decays to the long-run level)
    5. annualised vol      sigma * sqrt(252), to compare with India VIX
    6. a check             rolling out-of-sample: does the 95% band really hold 95% of the time?

Output : garch_results.json,  garch_series.csv (date, return, garch sigma, realised)
"""
import json, os, numpy as np, pandas as pd
from arch import arch_model

HERE = os.path.dirname(os.path.abspath(__file__))


def fit(r):
    """r: pandas Series of % returns.  -> fitted arch result (constant mean, GARCH(1,1), normal)"""
    return arch_model(r, mean="Constant", vol="GARCH", p=1, q=1, dist="normal").fit(disp="off")


def main():
    d = pd.read_csv(os.path.join(HERE, "nifty50_daily.csv"), parse_dates=["date"]).set_index("date")
    r = (100 * np.log(d.close / d.close.shift(1))).dropna()
    res = fit(r)
    p = res.params
    omega, alpha, beta, mu = p["omega"], p["alpha[1]"], p["beta[1]"], p["mu"]
    persist = alpha + beta
    half_life = np.log(0.5) / np.log(persist)
    uncond = np.sqrt(omega / (1 - persist))
    last_close = float(d.close.iloc[-1])

    # 1-4: forecasts from the last day
    fc = res.forecast(horizon=21)
    var_path = fc.variance.iloc[-1].values                 # h=1..21
    sig1 = float(np.sqrt(var_path[0]))
    term = {f"{h}d": float(np.sqrt(var_path[:h].sum())) for h in (1, 5, 10, 21)}   # sigma of the h-day return
    # tomorrow's band, built the SAME way as every historic band in the check below: centred on
    # mu (in log-return space), not on last_close with no drift.
    bands = {"68pct": [float(last_close * np.exp((mu - sig1) / 100)), float(last_close * np.exp((mu + sig1) / 100))],
             "95pct": [float(last_close * np.exp((mu - 1.96 * sig1) / 100)), float(last_close * np.exp((mu + 1.96 * sig1) / 100))]}
    var = {"95pct_loss_pct": float(1.645 * sig1 - mu), "99pct_loss_pct": float(2.326 * sig1 - mu)}

    # 6: the check.  Refit every 21 trading days on all data up to that point, forecast one day at a
    # time in between, and count how often the real return sat inside the 95% band.  Last 3 years.
    # Both mu and sigma come only from data up to the day before - a true out-of-sample check, with
    # no look-ahead of any kind (an earlier version centred every historic band on the FULL-SAMPLE
    # mu, which peeked at the future by a small amount; fixed here).
    start = len(r) - 756
    sig_oos, mu_oos, dates = [], [], []
    model = None
    for i in range(start, len(r)):
        if (i - start) % 21 == 0:
            model = fit(r.iloc[:i])
        # one-step-ahead mean and variance from the fitted parameters, applied to data up to i-1 only
        pr = model.params
        mu_oos.append(pr["mu"])
        e = (r.iloc[:i] - pr["mu"]).values
        s2 = e.var()
        for x in e:
            s2 = pr["omega"] + pr["alpha[1]"] * x * x + pr["beta[1]"] * s2
        sig_oos.append(np.sqrt(s2)); dates.append(r.index[i])
    sig_oos, mu_oos = np.array(sig_oos), np.array(mu_oos); real = r.iloc[start:].values
    inside95 = np.mean(np.abs(real - mu_oos) <= 1.96 * sig_oos)
    inside68 = np.mean(np.abs(real - mu_oos) <= sig_oos)
    # against the plain yardstick: 20-day historical volatility (centred on the same rolling mu)
    hv20 = r.rolling(20).std().shift(1).iloc[start:].values
    inside95_hv = np.mean(np.abs(real - mu_oos) <= 1.96 * hv20)
    # error of the volatility forecast itself, against |return| as the realised proxy
    mae_g = np.mean(np.abs(np.abs(real) - sig_oos * np.sqrt(2 / np.pi)))
    mae_h = np.mean(np.abs(np.abs(real) - hv20 * np.sqrt(2 / np.pi)))

    out = {"instrument": "NIFTY 50", "data_from": str(r.index[0].date()), "data_to": str(r.index[-1].date()), "days": int(len(r)),
           "last_close": last_close,
           "params": {"mu": float(mu), "omega": float(omega), "alpha": float(alpha), "beta": float(beta),
                      "persistence": float(persist), "half_life_days": float(half_life), "unconditional_sigma_pct": float(uncond),
                      "log_likelihood": float(res.loglikelihood)},
           "forecast": {"sigma_1d_pct": sig1, "sigma_1d_points": last_close * sig1 / 100, "annualised_vol_pct": sig1 * np.sqrt(252),
                        "term_structure_sigma_pct": term, "bands_next_close": bands, "one_day_var": var},
           "check_last_3y": {"days": int(len(real)), "inside_68_band": float(inside68), "inside_95_band": float(inside95),
                             "inside_95_band_hv20": float(inside95_hv), "vol_mae_garch": float(mae_g), "vol_mae_hv20": float(mae_h)},
           "summary_table": str(res.summary())}
    json.dump(out, open(os.path.join(HERE, "garch_results.json"), "w"), indent=1)
    pd.DataFrame({"date": dates, "return_pct": real, "mu_oos_pct": mu_oos, "garch_sigma_pct": sig_oos, "hv20_pct": hv20,
                 "abs_return": np.abs(real)}).to_csv(os.path.join(HERE, "garch_series.csv"), index=False)

    print(res.summary())
    print(f"\npersistence alpha+beta = {persist:.4f}   half-life = {half_life:.1f} days   long-run daily sigma = {uncond:.2f}%")
    print(f"\nFORECAST from {r.index[-1].date()} (close {last_close:,.2f})")
    print(f"  tomorrow's sigma        {sig1:.2f}%  = {last_close*sig1/100:.0f} points   annualised {sig1*np.sqrt(252):.1f}%")
    print(f"  68% band next close     {bands['68pct'][0]:,.0f} - {bands['68pct'][1]:,.0f}")
    print(f"  95% band next close     {bands['95pct'][0]:,.0f} - {bands['95pct'][1]:,.0f}")
    print(f"  1-day VaR 95% / 99%     {var['95pct_loss_pct']:.2f}% / {var['99pct_loss_pct']:.2f}%")
    print("  term structure sigma    " + "  ".join(f"{k} {v:.2f}%" for k, v in term.items()))
    print(f"\nCHECK, last {len(real)} days out-of-sample (refit every 21 days):")
    print(f"  inside 68% band {inside68:.1%}   inside 95% band {inside95:.1%}   (20-day HV band: {inside95_hv:.1%})")
    print(f"  volatility error  GARCH {mae_g:.3f}   20-day HV {mae_h:.3f}   (lower is better)")


if __name__ == "__main__":
    main()
