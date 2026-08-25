# Graph Report - megane  (2026-08-25)

## Corpus Check
- 167 files · ~65,725 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 981 nodes · 2017 edges · 74 communities (58 shown, 16 thin omitted)
- Extraction: 88% EXTRACTED · 12% INFERRED · 0% AMBIGUOUS · INFERRED: 236 edges (avg confidence: 0.85)
- Token cost: 0 input · 156,201 output

## Community Hubs (Navigation)
- Static Extraction & Schema Types
- LLM Analysis Response & Errors
- Schema Build Handler
- Server Bootstrap & Execution Stats
- Admin User & Project Handlers
- Schema Finalization & Analysis Storage
- Project Docs: Architecture & Onboarding
- Jump-Start Scaffolding
- Analyze Page Frontend (analyze.js)
- Crawler Material Selection Flow
- Crawler Interaction Flow (Click/XPath)
- File Converters: Image & Vision
- EDGAR Metadata Build Scripts (Python)
- Field Type CRUD & Hints
- Gemini Model Resolution
- Datamodeling Analysis Job Handler
- Type Sample Registration
- Pipeline Core (LLMOutput/Route/Run)
- App Frontend Shell (app.js)
- Crawler Popup & Imperative Steps
- Datamodeling Progress Tracking
- LLM Error Redaction
- LLM Concurrency Gate
- Work Queue
- Jump-Start Frontend Utils
- Excluded Schema Fields
- Ubuntu/Docker/AWS Install Script
- LLM Client Factory (Local Backend)
- Analyzer File Processing
- Gemini JSON Schema Builder
- Crawler Config & Logging
- Crawler Downloads
- Crawler Run Stats & Jobs
- Pipeline LLM Timing & Cancellation
- Claude-Flow MCP Config
- OpenAI LLM Backend
- Crawler Browser Launch (rod)
- LLM Route Config & Client
- Datamodeling Build Tests
- LLM Audit Gate
- Word File Converter
- Default DocumentAnalysis Output Model
- Gemini Model Catalog
- Prompt Selection Metadata
- Pipeline Queue Lifecycle
- EDGAR Primary-Doc Backfill Script
- Crawler Progress Hooks
- DWG/DXF File Converter
- Excel File Converter
- PDF File Converter
- Text File Converter
- Appraisal Output Model (generated)
- Bank Account Confirmation Output Model (generated)
- Payment Order Output Model (generated)
- Server Run Script
- Docker Build Script
- Docker Logs Script
- Docker Restart Script
- Dev Idea Briefs (EDGAR Port, Company Page)
- Go Module Definition
- Favicon (Battery Icon)

## God Nodes (most connected - your core abstractions)
1. `DB` - 76 edges
2. `BrowserFlow` - 30 edges
3. `BrowserSession` - 30 edges
4. `logStepDone()` - 21 edges
5. `logStep()` - 20 edges
6. `DMHandler` - 18 edges
7. `JumpStartConfig` - 18 edges
8. `PauseBetweenSteps()` - 17 edges
9. `ExcludeSchemaField()` - 16 edges
10. `FieldHint` - 14 edges

## Surprising Connections (you probably didn't know these)
- `Sample Service Agreement Testdata` --shares_data_with--> `Data Modeling Analyze Prompt`  [INFERRED]
  internal/datamodeling/testdata/sample_contract.txt → prompts/datamodeling_analyze.txt
- `Sample Invoice Testdata` --shares_data_with--> `Data Modeling Analyze Prompt`  [INFERRED]
  internal/datamodeling/testdata/sample_invoice.txt → prompts/datamodeling_analyze.txt
- `LLM Output Types Rationale` --conceptually_related_to--> `build-from-schema Skill`  [INFERRED]
  CLAUDE.md → AGENTS.md
- `main()` --calls--> `ResolveGeminiModel()`  [EXTRACTED]
  cmd/server/main.go → internal/datamodeling/model.go
- `main()` --calls--> `ResolvePath()`  [EXTRACTED]
  cmd/server/main.go → internal/db/db.go

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Agent/Sprout Onboarding Doc Index** — agents_sprout_agent_index, claude_project_overview, readme_megane_overview [EXTRACTED 1.00]
- **Data-Modeling-to-Generated-Prompt Pipeline** — prompts_datamodeling_analyze_prompt, generated_appraisal_prompts_appraisal_prompt, generated_bank_account_confirmation_prompts_bank_account_confirmation_prompt, generated_payment_order_prompts_payment_order_prompt [INFERRED 0.85]
- **Data Modeling LLM Prompt Suite** — prompts_datamodeling_analyze_prompt, prompts_datamodeling_refine_prompt, prompts_datamodeling_extract_fields_prompt, prompts_datamodeling_fix_json_prompt [INFERRED 0.85]

## Communities (74 total, 16 thin omitted)

### Community 0 - "Static Extraction & Schema Types"
Cohesion: 0.05
Nodes (59): StaticClient, FieldStat, ProposedSchema, SchemaStats, github.com/google/generative-ai-go/genai.Schema, reflect.StructField, reflect.Type, testing.T (+51 more)

### Community 1 - "LLM Analysis Response & Errors"
Cohesion: 0.06
Nodes (45): AnalyzeResponse, ConfidenceChange, ErrCostThresholdExceeded, ErrInvalidFileIDs, ErrInvalidLLMResponse, ErrNoSchema, ErrSchemaFieldNotFound, FieldChange (+37 more)

### Community 2 - "Schema Build Handler"
Cohesion: 0.07
Nodes (37): BuildHandler, ApplyOptions, Artifact, BuildArtifacts, BuildDetailView, BuildView, FieldOverride, FileTypeView (+29 more)

### Community 3 - "Server Bootstrap & Execution Stats"
Cohesion: 0.06
Nodes (26): getEnv(), main(), seedAdmin(), ExecutionStats, database/sql.Rows, github.com/gin-gonic/gin.Engine, github.com/gin-gonic/gin.HandlerFunc, sync.Mutex (+18 more)

### Community 4 - "Admin User & Project Handlers"
Cohesion: 0.08
Nodes (22): createUserRequest, Handler, Claims, github.com/gin-gonic/gin.Context, AuthHandler, FileHandler, loginRequest, slugify() (+14 more)

### Community 5 - "Schema Finalization & Analysis Storage"
Cohesion: 0.08
Nodes (22): Analysis, AnalysisSummaryRow, ConvergenceIndicators, PipelineEvent, User, database/sql.DB, BuildConvergenceIndicators(), CalculateConvergenceScore() (+14 more)

### Community 6 - "Project Docs: Architecture & Onboarding"
Cohesion: 0.08
Nodes (40): build-from-schema Skill, Jump-Start Scaffolding Feature, Agent / Sprout Index (AGENTS.md), Email+Password JWT Session Auth, Environment Variable Configuration (.env), File Pipeline Events Lifecycle, LLM Client Abstraction Rationale, LLM Output Types Rationale (+32 more)

### Community 7 - "Jump-Start Scaffolding"
Cohesion: 0.12
Nodes (34): envLine, JumpStartConfig, io/fs.DirEntry, buildEnvLines(), clamp(), copyFile(), copyLocalizedStatic(), CreateSprout() (+26 more)

### Community 8 - "Analyze Page Frontend (analyze.js)"
Cohesion: 0.11
Nodes (35): cancelProjectProcessing(), cellHtml(), cellText(), clearUploadProgress(), closeModal(), deleteFile(), destroyInsightCharts(), dropZone (+27 more)

### Community 9 - "Crawler Material Selection Flow"
Cohesion: 0.14
Nodes (10): BrowserSession, github.com/go-rod/rod.Page, BrowserFlow, logStep(), logStepDone(), Config, PauseBetweenSteps(), Config (+2 more)

### Community 10 - "Crawler Interaction Flow (Click/XPath)"
Cohesion: 0.11
Nodes (5): context.Context, Config, BrowserFlow, OpenBrowserFlow(), Pipeline

### Community 11 - "File Converters: Image & Vision"
Cohesion: 0.10
Nodes (18): Converter, ImageConverter, VisionPart, context.CancelFunc, GetConverter(), IsVisionMIME(), TestIsVisionMIME(), VisionImageFormat() (+10 more)

### Community 12 - "EDGAR Metadata Build Scripts (Python)"
Cohesion: 0.17
Nodes (23): Element, find_filing_dirs(), main(), Path, build_meta(), _build_summary(), _classify(), _classify_6k() (+15 more)

### Community 13 - "Field Type CRUD & Hints"
Cohesion: 0.16
Nodes (15): FieldHint, FileType, FileTypeSummary, GetFileTypeByID(), GetFileTypeBySlug(), ListFileTypes(), ListFileTypeSummaries(), UpdateFileTypeConfig() (+7 more)

### Community 14 - "Gemini Model Resolution"
Cohesion: 0.16
Nodes (15): github.com/google/generative-ai-go/genai.Client, ResolveGeminiModel(), TestResolveGeminiModel_default(), TestResolveGeminiModel_invalidFallsBack(), TestResolveGeminiModel_override(), EffectiveGeminiModelFromEnv(), geminiMajorVersion(), TestGeminiMajorVersion() (+7 more)

### Community 15 - "Datamodeling Analysis Job Handler"
Cohesion: 0.18
Nodes (13): DMHandler, AnalysisErrorBody, JobStatusResponse, ManualAnswer, ManualAnswerRequest, StoredAnalysisRequest, ClassifyAnalysisError(), CreateAnalysisJob() (+5 more)

### Community 16 - "Type Sample Registration"
Cohesion: 0.22
Nodes (15): DuplicateMatch, SampleRegisterInput, TypeSample, TypeSampleView, dmDetectMIME(), getUserID(), ResolveDatamodelingTimeout(), AnnotateFileProgressDuplicates() (+7 more)

### Community 17 - "Pipeline Core (LLMOutput/Route/Run)"
Cohesion: 0.14
Nodes (7): sync.Map, LLMOutput, Pipeline, Run, Run, Route, Queue

### Community 18 - "App Frontend Shell (app.js)"
Cohesion: 0.18
Nodes (11): apiFetch(), authHeaders(), authHeadersMultipart(), createLLMProgressBar(), formatDurationSeconds(), formatProcessingDisplay(), getToken(), isInProgressStatus() (+3 more)

### Community 19 - "Crawler Popup & Imperative Steps"
Cohesion: 0.17
Nodes (4): TwoStepDownloadOpts, time.Duration, BrowserFlow, BrowserFlow

### Community 20 - "Datamodeling Progress Tracking"
Cohesion: 0.29
Nodes (9): FileProgressItem, progressReporter, GetAnalysisJob(), BuildInitialFileProgress(), MarkJobProgressComplete(), MarkJobProgressError(), newProgressReporter(), ParseJobProgress() (+1 more)

### Community 21 - "LLM Error Redaction"
Cohesion: 0.17
Nodes (9): llmErrorDetail(), RedactSecrets(), SafeErr(), TestRedactSecrets_Bearer(), TestRedactSecrets_GeminiURL(), TestWrapSanitizedErr(), WrapSanitizedErr(), fakeErr (+1 more)

### Community 22 - "LLM Concurrency Gate"
Cohesion: 0.26
Nodes (9): Response, AcquireSlot(), CompleteGated(), InFlight(), initLLMSlots(), MaxConcurrent(), ReleaseSlot(), TestMaxConcurrentFromEnv() (+1 more)

### Community 23 - "Work Queue"
Cohesion: 0.21
Nodes (5): Queue, New(), TestQueueEnqueueRun(), Queue, Job

### Community 24 - "Jump-Start Frontend Utils"
Cohesion: 0.26
Nodes (9): collectJumpStartPayload(), collectLLMPayload(), doJumpStart(), jumpStartEscHtml(), LLM_PROVIDERS, renderLLMProviders(), showJumpStartResult(), updateLLMFields() (+1 more)

### Community 25 - "Excluded Schema Fields"
Cohesion: 0.38
Nodes (10): ExcludedField, Schema, GetLatestSchema(), addExcludedField(), ExcludedFieldNames(), ExcludeSchemaField(), MarshalExcludedFieldsJSON(), ParseExcludedFieldsJSON() (+2 more)

### Community 26 - "Ubuntu/Docker/AWS Install Script"
Cohesion: 0.45
Nodes (10): add_deploy_user_to_docker_group(), check_ubuntu(), die(), install_docker(), log(), main(), print_next_steps(), require_root() (+2 more)

### Community 27 - "LLM Client Factory (Local Backend)"
Cohesion: 0.22
Nodes (7): NormalizeEnvModel(), NewClient(), NewLocalClient(), NewOpenAIClient(), LocalClient, ollamaRequest, ollamaResponse

### Community 28 - "Analyzer File Processing"
Cohesion: 0.42
Nodes (4): AnalyzerConfig, fileEntry, Analyzer, NewAnalyzer()

### Community 29 - "Gemini JSON Schema Builder"
Cohesion: 0.42
Nodes (8): buildFieldSchema(), BuildJSONSchema(), buildObjectSchema(), buildProperties(), MarshalSchema(), DataModel, Field, FieldType

### Community 30 - "Crawler Config & Logging"
Cohesion: 0.28
Nodes (5): Backend, ProgressCallbacks, log/slog.Logger, DefaultConfig(), Config

### Community 31 - "Crawler Downloads"
Cohesion: 0.33
Nodes (6): net/http.Client, Config, HTTPDownload(), mimeParseContentDisposition(), SaveReader(), UniqueFilename()

### Community 32 - "Crawler Run Stats & Jobs"
Cohesion: 0.28
Nodes (5): AnalysisJob, CrawlerRunStat, CrawlerStatsSummary, time.Time, DB

### Community 33 - "Pipeline LLM Timing & Cancellation"
Cohesion: 0.31
Nodes (4): LLMInfo, LLMTiming, DB, llmTimingFromPipelineEvents()

### Community 34 - "Claude-Flow MCP Config"
Cohesion: 0.22
Nodes (8): CLAUDE_FLOW_HOOKS_ENABLED, CLAUDE_FLOW_MAX_AGENTS, CLAUDE_FLOW_MEMORY_BACKEND, CLAUDE_FLOW_MODE, CLAUDE_FLOW_TOPOLOGY, npm_config_update_notifier, npx, claude-flow

### Community 35 - "OpenAI LLM Backend"
Cohesion: 0.33
Nodes (6): openAIChoice, OpenAIClient, openAIMessage, openAIRequest, openAIResponse, openAIResponseFormat

### Community 36 - "Crawler Browser Launch (rod)"
Cohesion: 0.48
Nodes (6): github.com/go-rod/rod.Browser, chromeInstallCandidates(), Config, InstalledChromeBin(), LaunchBrowser(), resolveChromeExecutable()

### Community 37 - "LLM Route Config & Client"
Cohesion: 0.38
Nodes (5): LLMRouteConfig, Client, Request, RequestImage, RequestLog

### Community 38 - "Datamodeling Build Tests"
Cohesion: 0.38
Nodes (5): TestBuildApply_ReturnsArtifacts(), TestGetBuildDetail_FieldRowMerge(), TestUpsertBuildConfig_CreateAndUpdate(), CreateFileType(), CreateSchema()

### Community 39 - "LLM Audit Gate"
Cohesion: 0.38
Nodes (3): buildLLMAudit(), effectiveLLMModel(), llmAuditInfo

### Community 40 - "Word File Converter"
Cohesion: 0.40
Nodes (3): WordConverter, io.Reader, extractTextFromXML()

### Community 41 - "Default DocumentAnalysis Output Model"
Cohesion: 0.53
Nodes (4): DocumentAnalysis, LLMChartDataset, LLMChartSpec, LLMFileMetadata

### Community 42 - "Gemini Model Catalog"
Cohesion: 0.70
Nodes (4): catalogFallback(), dedupeSortedModels(), includeGeminiCatalogEntry(), ListGeminiGenerativeModels()

### Community 43 - "Prompt Selection Metadata"
Cohesion: 0.50
Nodes (3): Run, promptFileName(), PromptSelection

### Community 45 - "EDGAR Primary-Doc Backfill Script"
Cohesion: 0.60
Nodes (4): download(), find_real_gaps(), main(), One-off remediation: fetch primary documents that are referenced by filing.json…

## Knowledge Gaps
- **39 isolated node(s):** `npx`, `npm_config_update_notifier`, `CLAUDE_FLOW_MODE`, `CLAUDE_FLOW_HOOKS_ENABLED`, `CLAUDE_FLOW_TOPOLOGY` (+34 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **16 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `DB` connect `Schema Finalization & Analysis Storage` to `LLM Analysis Response & Errors`, `Schema Build Handler`, `Server Bootstrap & Execution Stats`, `Admin User & Project Handlers`, `Datamodeling Build Tests`, `Field Type CRUD & Hints`, `Datamodeling Analysis Job Handler`, `Type Sample Registration`, `Pipeline Core (LLMOutput/Route/Run)`, `Datamodeling Progress Tracking`, `Excluded Schema Fields`, `Analyzer File Processing`?**
  _High betweenness centrality (0.149) - this node is a cross-community bridge._
- **Why does `NewRouter()` connect `Server Bootstrap & Execution Stats` to `Schema Finalization & Analysis Storage`, `LLM Route Config & Client`, `Crawler Interaction Flow (Click/XPath)`, `Type Sample Registration`, `Pipeline Core (LLMOutput/Route/Run)`, `Analyzer File Processing`?**
  _High betweenness centrality (0.068) - this node is a cross-community bridge._
- **Why does `BuildSchema()` connect `Static Extraction & Schema Types` to `File Converters: Image & Vision`, `Analyzer File Processing`?**
  _High betweenness centrality (0.025) - this node is a cross-community bridge._
- **What connects `npx`, `npm_config_update_notifier`, `CLAUDE_FLOW_MODE` to the rest of the system?**
  _39 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Static Extraction & Schema Types` be split into smaller, more focused modules?**
  _Cohesion score 0.05271629778672032 - nodes in this community are weakly interconnected._
- **Should `LLM Analysis Response & Errors` be split into smaller, more focused modules?**
  _Cohesion score 0.06291591046581972 - nodes in this community are weakly interconnected._
- **Should `Schema Build Handler` be split into smaller, more focused modules?**
  _Cohesion score 0.06988120195667366 - nodes in this community are weakly interconnected._