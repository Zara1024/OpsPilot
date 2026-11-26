package main

import (
	managerbizknowledge "github.com/Zara1024/OpsPilot/internal/manager/biz/knowledge"
	manageralertdata "github.com/Zara1024/OpsPilot/internal/manager/data/alert/store"
	managerserveralert "github.com/Zara1024/OpsPilot/internal/manager/server/alert"
	managerserverknowledge "github.com/Zara1024/OpsPilot/internal/manager/server/knowledge"
	managerserversystemhealth "github.com/Zara1024/OpsPilot/internal/manager/server/systemhealth"
	managerserversystemupgrade "github.com/Zara1024/OpsPilot/internal/manager/server/systemupgrade"
	managersvcsystemhealth "github.com/Zara1024/OpsPilot/internal/manager/service/systemhealth"
	managersvcsystemupgrade "github.com/Zara1024/OpsPilot/internal/manager/service/systemupgrade"
	"github.com/Zara1024/OpsPilot/internal/pkg/config"
)

type systemModule struct {
	alertHandler         *managerserveralert.Handler
	systemHealthHandler  *managerserversystemhealth.Handler
	systemUpgradeHandler *managerserversystemupgrade.Handler
	knowledgeHandler     *managerserverknowledge.Handler
}

func bootstrapSystem(cfg *config.Config, storage *storageModule, iam *iamModule, settings *settingsModule, models *modelsModule, telemetry *telemetryModule, aiops *aiopsModule, knowledge *knowledgeModule, automation *automationModule, alerts *alertsModule) *systemModule {
	alertHandler := managerserveralert.NewHandler(aiops.alertSvc, aiops.alertSvc, aiops.alertSvc).
		WithInvestigations(manageralertdata.NewInvestigationRepo(storage.db)).
		WithEvidenceReader(aiops.aiopsRepo).
		WithRuntime(cfg.Alert.EvaluatorInterval, cfg.Alert.Cooldown)
	if alerts != nil && alerts.ingestSvc != nil {
		alertHandler.WithIngest(alerts.ingestSvc)
	}
	if automation.rcaInvConcrete != nil {
		alertHandler.WithInvestigationTrigger(automation.rcaInvConcrete)
	}
	var healthDB managersvcsystemhealth.DBPinger
	if storage.errDB == nil {
		healthDB = storage.sqlDB
	}
	systemHealthSvc := managersvcsystemhealth.New(managersvcsystemhealth.Config{
		Version:             version,
		PromEnabled:         cfg.Prom.Enabled,
		LogsEnabled:         cfg.Logs.URL != "",
		TracesEnabled:       cfg.Traces.URL != "",
		FrontierAddr:        cfg.FrontierClient.Addr,
		FrontierDisabled:    cfg.FrontierClient.Disabled,
		LLMConfigured:       cfg.OpenAI.APIKey != "",
		EmbeddingConfigured: knowledge.embErr == nil,
		QdrantURL:           knowledge.qdrantURL,
		QdrantCollection:    managerbizknowledge.CollectionName,
	}, managersvcsystemhealth.Dependencies{
		DB:      healthDB,
		Prom:    telemetry.promTester,
		Grafana: settings.grafanaSvc,
		Loki:    telemetry.lokiProbe,
		Tempo:   telemetry.tempoReadinessProbe,
		LLM:     models.llmSettingsResolver,
	})
	systemHealthHandler := managerserversystemhealth.NewHandler(systemHealthSvc)
	systemUpgradeSvc := managersvcsystemupgrade.New(managersvcsystemupgrade.Config{
		CurrentVersion: version,
	}, nil)
	systemUpgradeHandler := managerserversystemupgrade.NewHandler(systemUpgradeSvc)

	// HTTP handler for the knowledge base — built here, wired to routes
	// below. The biz Usecase + tool registry SetKnowledgeSearcher were
	// done earlier (before buildAIOpsRuntime) so the BaseTool bag picks
	// up query_knowledge.
	// knowledgeHandler may be nil if the embedder didn't initialize —
	// the route block below skips registration in that case.
	var knowledgeHandler *managerserverknowledge.Handler
	if knowledge.knowledgeUC != nil {
		knowledgeHandler = managerserverknowledge.NewHandler(knowledge.knowledgeUC)
		knowledgeHandler.SetAuthz(iam.authzMW)
	}

	return &systemModule{
		alertHandler:         alertHandler,
		systemHealthHandler:  systemHealthHandler,
		systemUpgradeHandler: systemUpgradeHandler,
		knowledgeHandler:     knowledgeHandler,
	}
}
