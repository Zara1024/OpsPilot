import { useState, useEffect, useCallback } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import {
  ChevronLeft,
  RefreshCw,
  Webhook,
  Activity,
  PhoneCall,
} from 'lucide-react';
import {
  Button,
  PageHeader,
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from '@/components/ui';
import { useI18n } from '@/i18n/locale';
import { cn } from '@/lib/cn';
import { useAssistantPage } from '@/lib/assistantPage';
import {
  listIngestSources,
  listHeartbeats,
  type IngestSource,
  type Heartbeat,
} from '@/api/alerts';
import { IngestSourcesTab } from './alerts/IngestSourcesTab';
import { HeartbeatsTab } from './alerts/HeartbeatsTab';

export default function AlertIntegrationsPage() {
  const { tr } = useI18n();
  const [searchParams, setSearchParams] = useSearchParams();
  const currentTab = searchParams.get('tab') === 'heartbeats' ? 'heartbeats' : 'sources';

  const [sources, setSources] = useState<IngestSource[]>([]);
  const [heartbeats, setHeartbeats] = useState<Heartbeat[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const fetchAll = useCallback(async (silent = false) => {
    if (!silent) setLoading(true);
    else setRefreshing(true);
    try {
      const [srcRes, hbRes] = await Promise.all([
        listIngestSources(),
        listHeartbeats(),
      ]);
      setSources(srcRes.items || []);
      setHeartbeats(hbRes.items || []);
      setLoadError(null);
    } catch (e: unknown) {
      setLoadError(e instanceof Error ? e.message : tr('加载外部告警接入数据失败', 'Failed to load integrations'));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, [tr]);

  useEffect(() => {
    void fetchAll(false);
  }, [fetchAll]);

  useAssistantPage('外部告警接入', 'Alert Integrations', {
    tab: currentTab,
    source_count: sources.length,
    heartbeat_count: heartbeats.length,
  });

  return (
    <main className="anim-fade flex flex-1 flex-col overflow-hidden bg-bg">
      <Tabs
        value={currentTab}
        onValueChange={(val) => setSearchParams({ tab: String(val) })}
        className="flex flex-1 flex-col overflow-hidden"
      >
        <PageHeader
          title={
            <div className="space-y-1">
              <div className="flex items-center gap-2 text-xs text-text-muted">
                <Link to="/alerts" className="inline-flex items-center gap-1 hover:text-text">
                  <ChevronLeft size={12} /> {tr('返回告警列表', 'Back to Alerts')}
                </Link>
              </div>
              <div className="flex items-center gap-2">
                <Webhook size={18} className="text-indigo-500" />
                <span>{tr('外部告警接入与死信开关', 'External Alert Ingest & Heartbeats')}</span>
              </div>
            </div>
          }
          subtitle={tr(
            `接入源 ${sources.length} 个 · 死信心跳 ${heartbeats.length} 个`,
            `${sources.length} ingest sources · ${heartbeats.length} heartbeats`,
          )}
          actions={
            <div className="flex flex-wrap items-center gap-2">
              <Link
                to="/oncall?tab=routes"
                data-slot="button"
                data-variant="outline"
                data-size="sm"
                className="og-button inline-flex items-center gap-1.5 no-underline"
              >
                <PhoneCall size={13} />
                {tr('前往 On-Call 路由', 'On-Call Routing')}
              </Link>
              <Button
                size="sm"
                variant="outline"
                onClick={() => void fetchAll(true)}
                disabled={loading || refreshing}
              >
                <RefreshCw size={13} className={cn('mr-1.5', (loading || refreshing) && 'animate-spin')} />
                {tr('刷新', 'Refresh')}
              </Button>
            </div>
          }
          extra={
            <TabsList>
              <TabsTrigger value="sources">
                <Webhook size={14} className="mr-1.5" />
                {tr('外部接入源', 'Ingest Sources')}
                {sources.length > 0 && (
                  <span className="ml-1.5 rounded-full bg-indigo-500/15 px-1.5 py-0.5 text-[10px] font-medium text-indigo-600 dark:text-indigo-400">
                    {sources.length}
                  </span>
                )}
              </TabsTrigger>
              <TabsTrigger value="heartbeats">
                <Activity size={14} className="mr-1.5" />
                {tr('死信心跳监控', 'Heartbeats')}
                {heartbeats.length > 0 && (
                  <span className="ml-1.5 rounded-full bg-emerald-500/15 px-1.5 py-0.5 text-[10px] font-medium text-emerald-600 dark:text-emerald-400">
                    {heartbeats.length}
                  </span>
                )}
              </TabsTrigger>
            </TabsList>
          }
        />

        <div className="flex-1 min-w-0 overflow-y-auto px-6 py-6 space-y-6">
          {loadError && (
            <div
              role="alert"
              className="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-500/30 bg-red-500/10 px-4 py-3 text-xs text-red-700 dark:text-red-300"
            >
              <span>{loadError}</span>
              <Button size="sm" variant="outline" onClick={() => void fetchAll(false)}>
                {tr('重试', 'Retry')}
              </Button>
            </div>
          )}

          <TabsContent value="sources">
            <IngestSourcesTab
              sources={sources}
              loading={loading}
              onChanged={() => void fetchAll(true)}
            />
          </TabsContent>

          <TabsContent value="heartbeats">
            <HeartbeatsTab
              heartbeats={heartbeats}
              loading={loading}
              onChanged={() => void fetchAll(true)}
            />
          </TabsContent>
        </div>
      </Tabs>
    </main>
  );
}
