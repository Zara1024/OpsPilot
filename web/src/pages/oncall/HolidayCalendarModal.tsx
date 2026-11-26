import { useState, useEffect, useCallback } from 'react';
import { Calendar, Plus, Trash2, RefreshCw, DownloadCloud } from 'lucide-react';
import {
  Button,
  Chip,
  EmptyState,
  Input,
  Label,
  Select,
} from '@/components/ui';
import { useDialogs } from '@/components/ui/useDialogs';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import { usePermissions } from '@/store/me';
import {
  listCalendarDays,
  upsertCalendarDay,
  syncCNCalendarDays,
  deleteCalendarDay,
  type CalendarDayItem,
} from '@/api/oncall';

interface HolidayCalendarModalProps {
  open: boolean;
  onClose: () => void;
  onUpdated?: () => void;
}

export function HolidayCalendarModal({
  open,
  onClose,
  onUpdated,
}: HolidayCalendarModalProps) {
  const { tr } = useI18n();
  const { isAdmin } = usePermissions();
  const { confirmAction, alertAction, dialog } = useDialogs();

  const [year, setYear] = useState<number>(2026);
  const [days, setDays] = useState<CalendarDayItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [syncing, setSyncing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [feedback, setFeedback] = useState<string | null>(null);

  // Custom date override form
  const [customDate, setCustomDate] = useState('2026-07-01');
  const [customType, setCustomType] = useState<'holiday' | 'workday'>('holiday');
  const [customName, setCustomName] = useState('');

  const fetchDays = useCallback(async () => {
    if (!open) return;
    setLoading(true);
    try {
      const res = await listCalendarDays(year, 'CN');
      setDays(res.items || []);
    } catch (e: unknown) {
      console.error('Failed to load calendar days:', e);
    } finally {
      setLoading(false);
    }
  }, [open, year]);

  useEffect(() => {
    void fetchDays();
  }, [fetchDays]);

  const handleSyncCN = async () => {
    if (!isAdmin || syncing) return;
    setSyncing(true);
    setFeedback(null);
    try {
      const res = await syncCNCalendarDays(year);
      setFeedback(
        tr(
          `已同步 ${res.year} 年国务院法定节假日及调休安排（共 ${res.synced_count} 天）。`,
          `Synced ${res.synced_count} official CN holiday/makeup workday entries for ${res.year}.`
        )
      );
      await fetchDays();
      onUpdated?.();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('同步节假日失败', 'Failed to sync holidays'));
    } finally {
      setSyncing(false);
    }
  };

  const handleAddCustom = async () => {
    if (!isAdmin || saving) return;
    if (!customDate.trim()) {
      await alertAction(tr('请选择日期', 'Date is required'));
      return;
    }
    setSaving(true);
    setFeedback(null);
    try {
      await upsertCalendarDay({
        date: customDate.trim(),
        day_type: customType,
        name: customName.trim() || (customType === 'holiday' ? tr('自定义节假日', 'Custom Holiday') : tr('自定义调休上班', 'Custom Workday')),
        region: 'CN',
      });
      setCustomName('');
      setFeedback(tr('已保存自定义日历日期规则。', 'Custom calendar override saved.'));
      await fetchDays();
      onUpdated?.();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('保存自定义日历失败', 'Failed to save override'));
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async (item: CalendarDayItem) => {
    if (!isAdmin || !item.id) return;
    const confirmed = await confirmAction(
      tr(`确认删除自定义日历记录 ${item.date} (${item.name}) 吗？`, `Delete calendar override ${item.date} (${item.name})?`),
    );
    if (!confirmed) return;
    try {
      await deleteCalendarDay(item.id);
      await fetchDays();
      onUpdated?.();
    } catch (e: unknown) {
      await alertAction(e instanceof Error ? e.message : tr('删除失败', 'Failed to delete'));
    }
  };

  return (
    <>
      <Modal
        open={open}
        onClose={onClose}
        title={tr('中国法定节假日与调休日历', 'Statutory Holiday & Makeup Workday Calendar')}
        size="lg"
        footer={
          <div className="flex justify-end w-full">
            <Button size="sm" variant="outline" onClick={onClose}>
              {tr('关闭', 'Close')}
            </Button>
          </div>
        }
      >
        <div className="space-y-4 py-1 text-xs">
          {/* Toolbar */}
          <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-border bg-bg/50 p-3">
            <div className="flex items-center gap-2">
              <Label className="text-xs">{tr('年份', 'Year')}</Label>
              <Select
                value={String(year)}
                onValueChange={(v) => {
                  const nextY = Number(v);
                  setYear(nextY);
                  setCustomDate(`${nextY}-07-01`);
                }}
                options={[
                  { value: '2025', label: '2025' },
                  { value: '2026', label: '2026' },
                  { value: '2027', label: '2027' },
                ]}
                className="w-28 text-xs"
              />
              <Button
                size="sm"
                variant="ghost"
                onClick={fetchDays}
                disabled={loading}
                aria-label={tr('刷新节假日列表', 'Refresh holidays')}
              >
                <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
              </Button>
            </div>
            {isAdmin && (
              <Button
                size="sm"
                variant="outline"
                onClick={handleSyncCN}
                disabled={syncing || year === 2027}
                title={year === 2027 ? tr('2027 年官方放假安排尚未发布，请在下方自定义添加', '2027 official calendar not yet published') : ''}
              >
                <DownloadCloud size={14} className="mr-1.5" />
                {syncing ? tr('同步中...', 'Syncing...') : tr('同步国务院法定节假日预设', 'Sync Official CN Presets')}
              </Button>
            )}
          </div>

          {feedback && (
            <div className="rounded-md border border-emerald-500/30 bg-emerald-500/10 px-3 py-2 text-xs text-emerald-700 dark:text-emerald-300">
              {feedback}
            </div>
          )}

          {/* Custom Add / Override Form (Admin only) */}
          {isAdmin && (
            <div className="rounded-lg border border-border p-3 space-y-2">
              <div className="font-semibold text-text">
                {tr('添加或覆盖自定义放假 / 调休工作日', 'Add or Override Custom Holiday / Workday')}
              </div>
              <div className="grid grid-cols-1 sm:grid-cols-4 gap-2 items-end">
                <div>
                  <Label className="text-xs">{tr('日期', 'Date')}</Label>
                  <Input
                    type="date"
                    value={customDate}
                    onChange={(e) => setCustomDate(e.target.value)}
                    className="mt-1 text-xs font-mono"
                  />
                </div>
                <div>
                  <Label className="text-xs">{tr('类型', 'Day Type')}</Label>
                  <Select
                    value={customType}
                    onValueChange={(v) => setCustomType(v as 'holiday' | 'workday')}
                    options={[
                      { value: 'holiday', label: tr('放假日（跳过工作日排班）', 'Holiday (Off)') },
                      { value: 'workday', label: tr('补班工作日（含周末调休）', 'Workday (Makeup)') },
                    ]}
                    className="mt-1 w-full text-xs"
                  />
                </div>
                <div>
                  <Label className="text-xs">{tr('说明名称', 'Label')}</Label>
                  <Input
                    value={customName}
                    onChange={(e) => setCustomName(e.target.value)}
                    placeholder={tr('如：公司司庆调休', 'e.g. Company Holiday')}
                    className="mt-1 text-xs"
                  />
                </div>
                <Button size="sm" variant="primary" onClick={handleAddCustom} disabled={saving}>
                  <Plus size={14} className="mr-1" />
                  {saving ? tr('保存中...', 'Saving...') : tr('保存规则', 'Save Date')}
                </Button>
              </div>
            </div>
          )}

          {/* Calendar Table */}
          <div className="rounded-lg border border-border overflow-hidden max-h-80 overflow-y-auto">
            {days.length === 0 ? (
              <EmptyState
                icon={Calendar}
                title={tr(`${year} 年暂无节假日与调休规则`, `No holiday rules for ${year}`)}
                hint={tr(
                  '可点击上方按钮同步内置安排或添加自定义日期覆盖。',
                  'Sync official presets or add custom dates above.'
                )}
              />
            ) : (
              <table className="w-full text-left border-collapse text-xs text-text">
                <thead className="sticky top-0 bg-bg border-b border-border text-text-muted">
                  <tr>
                    <th className="py-2 px-3 font-medium">{tr('日期', 'Date')}</th>
                    <th className="py-2 px-3 font-medium">{tr('类型', 'Type')}</th>
                    <th className="py-2 px-3 font-medium">{tr('名称', 'Name')}</th>
                    <th className="py-2 px-3 font-medium">{tr('来源', 'Source')}</th>
                    {isAdmin && <th className="py-2 px-3 font-medium text-left">{tr('操作', 'Action')}</th>}
                  </tr>
                </thead>
                <tbody className="divide-y divide-border">
                  {days.map((d) => (
                    <tr key={`${d.date}-${d.id ?? 'builtin'}`} className="hover:bg-bg/40">
                      <td className="py-2 px-3 font-mono whitespace-nowrap">{d.date}</td>
                      <td className="py-2 px-3 whitespace-nowrap">
                        <Chip tone={d.day_type === 'holiday' ? 'warning' : 'accent'} dense>
                          {d.day_type === 'holiday' ? tr('法定放假', 'Holiday') : tr('调休补班', 'Workday')}
                        </Chip>
                      </td>
                      <td className="py-2 px-3">{d.name || '-'}</td>
                      <td className="py-2 px-3">
                        <Chip tone={d.source === 'custom' ? 'info' : 'default'} dense>
                          {d.source === 'custom' ? tr('自定义', 'Custom') : tr('系统预设', 'Preset')}
                        </Chip>
                      </td>
                      {isAdmin && (
                        <td className="py-2 px-3 text-left">
                          {d.id ? (
                            <Button
                              size="sm"
                              variant="dangerGhost"
                              onClick={() => handleDelete(d)}
                              aria-label={tr('删除日期规则', 'Delete date rule')}
                            >
                              <Trash2 size={12} />
                            </Button>
                          ) : (
                            <span className="text-text-faint">-</span>
                          )}
                        </td>
                      )}
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </Modal>
      {dialog}
    </>
  );
}
