package main

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"gorm.io/gorm"

	tokenbiz "github.com/Zara1024/OpsPilot/internal/iam/biz/integrationtoken"
	tokendata "github.com/Zara1024/OpsPilot/internal/iam/data/integrationtoken"
	userdata "github.com/Zara1024/OpsPilot/internal/iam/data/user/sqlite"
	iamserver "github.com/Zara1024/OpsPilot/internal/iam/server"
	aiopstools "github.com/Zara1024/OpsPilot/internal/manager/biz/aiops/tools"
	managerbizapproval "github.com/Zara1024/OpsPilot/internal/manager/biz/approval"
	managerauditbiz "github.com/Zara1024/OpsPilot/internal/manager/biz/audit"
	managerbizdevice "github.com/Zara1024/OpsPilot/internal/manager/biz/device"
	managerbizedge "github.com/Zara1024/OpsPilot/internal/manager/biz/edge"
	biz "github.com/Zara1024/OpsPilot/internal/manager/biz/externalmcp"
	managerbizmonitor "github.com/Zara1024/OpsPilot/internal/manager/biz/monitor"
	data "github.com/Zara1024/OpsPilot/internal/manager/data/externalmcp"
	managerserverapproval "github.com/Zara1024/OpsPilot/internal/manager/server/approval"
	server "github.com/Zara1024/OpsPilot/internal/manager/server/externalmcp"
	managersvcalert "github.com/Zara1024/OpsPilot/internal/manager/service/alert"
	service "github.com/Zara1024/OpsPilot/internal/manager/service/externalmcp"
	managersvcfb "github.com/Zara1024/OpsPilot/internal/manager/service/frontierbound"
	managersvck8s "github.com/Zara1024/OpsPilot/internal/manager/service/k8s"
	"github.com/Zara1024/OpsPilot/internal/pkg/config"
)

type externalMCPModule struct {
	tokens  *iamserver.TokenHandler
	handler *server.Handler
	engine  *biz.Engine
}

func bootstrapExternalMCP(
	cfg *config.Config,
	log *slog.Logger,
	reg *prometheus.Registry,
	db *gorm.DB,
	auditUC *managerauditbiz.Usecase,
	deviceUC *managerbizdevice.Usecase,
	edgeUC *managerbizedge.Usecase,
	k8sSvc *managersvck8s.Service,
	alertSvc *managersvcalert.Service,
	monitorSvc *managerbizmonitor.Service,
	fbClient *managersvcfb.Client,
	toolsReg *aiopstools.Registry,
	approvalUC *managerbizapproval.Usecase,
	approvalHandler *managerserverapproval.Handler,
) (*externalMCPModule, error) {
	auth := tokenbiz.New(tokendata.New(db), userdata.NewRepo(db), auditUC)
	baseURL := strings.TrimRight(cfg.PublicURL, "/")
	m := &externalMCPModule{tokens: iamserver.NewTokenHandler(auth, cfg.ExternalMCP.Enabled, cfg.ExternalMCP.WritesEnabled, baseURL+"/api/v1/integrations/mcp")}
	if !cfg.ExternalMCP.Enabled {
		return m, nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1" || u.Hostname() == "::1"))) {
		return nil, fmt.Errorf("external MCP requires an HTTPS OPSPILOT_PUBLIC_URL (HTTP loopback is allowed for local tests)")
	}
	for _, origin := range cfg.ExternalMCP.AllowedOrigins {
		u, err := url.Parse(origin)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("invalid external MCP origin")
		}
	}
	store := data.New(db)
	engine := &biz.Engine{Auth: auth, Audit: auditUC, Store: store, Approvals: approvalUC, WritesEnabled: cfg.ExternalMCP.WritesEnabled, BaseURL: baseURL, Log: log.With("component", "external-mcp")}
	baseTools := toolsReg.BuildBaseTools().AllTools()
	catalog, err := service.QueryTools(baseTools)
	if err != nil {
		return nil, err
	}
	builtinWrites, err := service.BuiltinWriteTools(baseTools)
	if err != nil {
		return nil, err
	}
	catalog = append(catalog, builtinWrites...)
	resources := service.Resources{Devices: deviceUC, Alerts: alertSvc, Monitor: monitorSvc}
	catalog = append(catalog, resources.Tools()...)
	if !cfg.FrontierClient.Disabled {
		actions, err := service.ActionTools(aiopstools.NewRestartServiceTool(fbClient, edgeUC, deviceUC, log), aiopstools.NewPersistentK8sActionTool(fbClient, k8sSvc, log, store))
		if err != nil {
			return nil, err
		}
		catalog = append(catalog, actions...)
	}
	catalog = append(service.ProjectTools(catalog), engine.ControlTools()...)
	engine.Catalog, err = biz.NewCatalog(catalog)
	if err != nil {
		return nil, err
	}
	approvalUC.RegisterExecutor(biz.ApprovalKind, engine.Approve)
	approvalHandler.SetExternalOperations(engine)
	m.handler = server.New(engine, cfg.ExternalMCP.AllowedOrigins, reg)
	m.engine = engine
	return m, nil
}

func (m *externalMCPModule) addAPITools(router chi.Router) error {
	tools, err := server.APITools(router, m.engine.WritesEnabled)
	if err != nil {
		return err
	}
	for _, tool := range m.engine.Catalog {
		tools = append(tools, *tool)
	}
	catalog, err := biz.NewCatalog(tools)
	if err != nil {
		return err
	}
	m.engine.Catalog = catalog
	return nil
}
