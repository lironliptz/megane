# HLD: Event-Driven Stock Price Prediction & Correlation Engine

Design for `prompts/dev/prompt_12_suggest_prediction_mechanism.txt`.

Triage: **HEAVY** — new SQLite tables for historical consensus and prediction results, a new Go package (`internal/prediction/`), a new company tab (`#predictions`), a new training/backtesting CLI, and a dual-engine predictive framework combining Go-native machine learning with Gemini LLM qualitative synthesis.

---

## 1. Objective

Build a robust, event-driven predictive modeling framework that correlates multi-modal corporate events (SEC filings, financial surprises, analyst consensus revisions) with subsequent short-term stock price movements (3-day and 5-day post-event returns), enabling users to discover statistically significant market patterns and view real-time forecasts for active events.

---

## 2. Context — what exists today

| Layer | State | Path |
|---|---|---|
| Company identity | `submissions.json`; Kamada carries ticker **KMDA** (Nasdaq) | `fileDB/companies/{cik}/` |
| Filing corpus | `meta.json`, `financials.json` (prompts 8–9) | `{year}/{accession}/` |
| Timeline | Two marker bands on a hidden `yEvents` axis: financial 0.93–0.98, general 0.82–0.90 (prompt 10, **shipped**) | `internal/companyview/` |
| Analyst consensus | `company_analysts`, `analyst_reports`, `analyst_period_consensus` (prompt 11, **shipped**) | `internal/db/` |
| Market data | Historical daily close, volume, adjusted close, and volatility | `internal/marketdata/` |
| Prediction code | **none** | — |

---

## 3. Data Audit & Constraints

### 3.1 Temporal Alignment & Lookahead Bias
The most critical risk in financial predictive modeling is **lookahead bias** (using future data to predict the past). To guarantee mathematical validity:
* All features for an event at $T_0$ must be calculated using information available strictly at or before the market close of $T_0$.
* The target return window must begin strictly on $T_{+1}$ and end on $T_{+3}$ or $T_{+5}$.
* If $T_0$ is a non-trading day, $T_0$ snaps forward to the next trading day, and the pre-announcement baseline price is taken from the preceding trading day ($T_{-1}$).

### 3.2 Cross-Sectional Training Scale
Because individual companies have sparse event histories (e.g., Kamada has only 4 earnings reports per year), training a model on a single company's history will lead to severe overfitting. The model must be trained **cross-sectionally** across the entire multi-company database in `fileDB` to learn generalizable market principles.

### 3.3 Benchmark Index Normalization
Raw stock returns are highly influenced by overall market regimes (bull vs. bear markets). To isolate the event's true impact, the target variable must be normalized to **Cumulative Abnormal Return (CAR)** by subtracting the benchmark index return (e.g., NASDAQ-100 or S&P 500) over the same window.

---

## 4. Architecture Decisions

### D1 — Dual-Engine Predictor (Quantitative + Qualitative)

To achieve both high statistical accuracy and rich interpretability, the engine implements a **Dual-Engine Architecture**:

1. **Quantitative Engine (Go-Native):**
   - Implemented in `internal/prediction/quantitative.go`.
   - Uses a Go-native Ridge Regression or Random Forest model trained on cross-sectional historical event-feature vectors.
   - Outputs a predicted return magnitude and a statistical confidence interval.
2. **Qualitative Engine (Gemini LLM):**
   - Implemented in `internal/prediction/qualitative.go`.
   - Invokes Gemini using structured JSON output via `llm.Client`.
   - Prompt passes the current event's feature vector, the historical correlation coefficients of those features, and the event's textual summary.
   - Outputs a structured prediction: direction (Bullish/Bearish/Neutral), probability, and a 2-sentence investment thesis.

### D2 — Feature Vector Construction

A unified struct `FeatureVector` represents the multi-modal features extracted around an event at $T_0$:

```go
type FeatureVector struct {
	EventID               string    `json:"eventId"`
	CIK                   string    `json:"cik"`
	EventDate             string    `json:"eventDate"`
	FormType              string    `json:"formType"`              // e.g., "10-Q", "8-K"
	EventCategory         string    `json:"eventCategory"`          // e.g., "financial", "governance"
	EventWeight           int       `json:"eventWeight"`            // 1 (Minor), 2 (Medium), 3 (Major)
	RevenueSurprisePct    float64   `json:"revenueSurprisePct"`     // actual vs consensus
	EPSSurprisePct        float64   `json:"epsSurprisePct"`         // actual vs consensus
	ConsensusPTChange30d  float64   `json:"consensusPtChange30d"`   // analyst PT momentum
	RatingUpgrade         int       `json:"ratingUpgrade"`          // +1 (Upgrade), -1 (Downgrade), 0 (Reiterate)
	HistoricalVolatility  float64   `json:"historicalVolatility"`   // 30-day market volatility
	PriceMomentum30d      float64   `json:"priceMomentum30d"`       // 30-day stock return
	VolumeSpikeRatio      float64   `json:"volumeSpikeRatio"`       // Vol(T0) / MA(Vol, 30d)
	TargetCAR5d           float64   `json:"targetCAR5d"`            // Cumulative Abnormal Return (Target)
}
```

### D3 — Statistical Correlation Engine

The correlation engine (`internal/prediction/correlation.go`) calculates the Pearson and Spearman rank correlation coefficients between each numerical feature and the target variable (`TargetCAR5d`) over all historical samples:

$$\rho = \frac{\text{Cov}(X, Y)}{\sigma_X \sigma_Y}$$

Features with a $p\text{-value} < 0.05$ are flagged as **statistically significant** and highlighted in the UI.

### D4 — SQLite Tables (Migration v35)

```sql
-- Store extracted feature vectors for historical events
CREATE TABLE IF NOT EXISTS event_features (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL,
  cik TEXT NOT NULL,
  event_date TEXT NOT NULL,
  form_type TEXT NOT NULL,
  event_category TEXT NOT NULL,
  event_weight INTEGER NOT NULL,
  revenue_surprise_real REAL,
  eps_surprise_real REAL,
  consensus_pt_change_30d REAL,
  rating_upgrade INTEGER NOT NULL,
  historical_volatility REAL NOT NULL,
  price_momentum_30d REAL NOT NULL,
  volume_spike_ratio REAL NOT NULL,
  target_car_5d REAL,                 -- NULL for active/recent events
  UNIQUE(cik, event_id)
);

-- Store model prediction outputs for active and historical events
CREATE TABLE IF NOT EXISTS prediction_results (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id TEXT NOT NULL,
  cik TEXT NOT NULL,
  predicted_direction TEXT NOT NULL,  -- bullish | bearish | neutral
  probability REAL NOT NULL,          -- 0.0 to 1.0
  expected_return_5d REAL NOT NULL,
  confidence_interval_low REAL NOT NULL,
  confidence_interval_high REAL NOT NULL,
  thesis TEXT NOT NULL,
  feature_contributions_json TEXT NOT NULL, -- JSON string of feature contributions
  created_at TEXT NOT NULL,
  UNIQUE(cik, event_id)
);
```

### D5 — API Specifications

All endpoints are authenticated and return the standard `{"data": ..., "error": null}` envelope.

| Method | Path | Notes |
|---|---|---|
| GET | `/api/companies/:cik/predictions/correlations` | Returns feature correlation coefficients and p-values |
| GET | `/api/companies/:cik/predictions/active` | Returns active predictions for events in the last 5 days |
| GET | `/api/companies/:cik/predictions/backtest` | Returns historical model accuracy and metrics |

### D6 — Walk-Forward Backtesting

To measure the model's true performance without temporal leakage, the backtesting engine (`internal/prediction/backtest.go`) implements a **walk-forward validation** scheme:
1. Sort all historical events chronologically.
2. For each historical event $E_i$ occurring at date $D_i$:
   - Train the model using only events occurring strictly before $D_i$.
   - Predict the return for $E_i$.
   - Compare the prediction with the actual subsequent 5-day return.
3. Calculate aggregate metrics: Directional Accuracy (Hit Rate), Mean Absolute Error (MAE), and Sharpe Ratio of the event-driven strategy.

---

## 5. What this touches

| Area | New / Changed |
|---|---|
| `internal/db/db.go` | **+1 migration (v35)** |
| `internal/db/predictions.go` | **new** — query helpers for feature vectors, correlations, and results |
| `internal/prediction/` | **new** — `correlation.go`, `quantitative.go`, `qualitative.go`, `backtest.go`, `pipeline.go` |
| `internal/handlers/companies.go`, `router.go` | 3 GET routes registered under the `apiCompanies` group |
| `cmd/predict-train/` | **new** — CLI to trigger cross-sectional training and backtesting |
| `static/company.html`, `shell.js`, `tab-predictions.js`, `style.css` | sixth tab (Predictions) |

---

## 6. Out of scope

- High-frequency or intra-day trading indicators.
- Automated order placement or brokerage integration.
- Sentiment analysis of unstructured forums or social media.
- Training models on external cloud infrastructure (all training is local and fast).

---

## 7. Risks

| Risk | Severity | Mitigation |
|---|---|---|
| **Data Leakage (Lookahead Bias)** | **High** | Strictly enforce that target returns start on $T_{+1}$ and feature extraction uses data $\le T_0$ only. |
| **Overfitting on Small Sample Sizes** | **High** | Enforce cross-sectional training across all companies in `fileDB` instead of single-ticker models. |
| **Market Regime Shifts** | Medium | Include benchmark index returns ($R_{\text{market}}$) to calculate Cumulative Abnormal Returns (CAR) instead of raw returns. |
| **Illiquid Stock Noise** | Medium | Filter out training samples from companies with average daily volume below 50K shares. |

---

## 8. Done When

Open `/company/0001567529#predictions`. The tab shows:
1. **Active Prediction Card:** If a major event occurred within the last 5 days, displays the predicted direction, probability, expected 5-day return, and the Gemini-generated thesis.
2. **Correlation Heatmap:** A visual grid showing the Pearson correlation coefficients of various event features with subsequent 5-day returns.
3. **Backtest Performance Panel:** Displays the model's historical directional accuracy (e.g., "76% Correct") and a table comparing historical predictions against actual returns.

---

## 9. Deliverables & Phasing

1. `prompt_12_suggest_prediction_mechanism-lld.md` — Detailed LLD describing the exact Go ML library, training serialization format, and Gemini JSON schema.
2. **P0 (Feature Pipeline & Correlation)**: Migration v35, feature extraction pipeline, and statistical correlation engine.
3. **P1 (Dual-Engine & API)**: Quantitative Go ML model, Gemini qualitative agent, and the three GET endpoints.
4. **P1 (Predictions Tab)**: Frontend implementation of the `#predictions` tab (Correlation Heatmap, Active Prediction Card, Backtest Performance).
5. **P2 (Training CLI)**: `cmd/predict-train` CLI for bulk training and model serialization.
6. **P3 (Backtesting Engine)**: Walk-forward validation engine and performance metrics dashboard.
