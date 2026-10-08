import { useState, useEffect, useCallback } from 'react';
import {
  UserCheck,
  Check,
  X,
  Clock,
  RefreshCw,
  AlertCircle,
  Calendar,
  UserX,
} from 'lucide-react';
import {
  Button,
  Card,
  Chip,
  EmptyState,
  Input,
  Label,
  Select,
} from '@/components/ui';
import { Modal } from '@/components/Modal';
import { useI18n } from '@/i18n/locale';
import {
  listPendingOverrides,
  approveOverride,
  rejectOverride,
  type PendingOverride,
} from '@/api/oncall';

interface SwapApprovalsTabProps {
  onPendingCountChange?: (count: number) => void;
}

export function SwapApprovalsTab({ onPendingCountChange }: SwapApprovalsTabProps) {
  const { tr } = useI18n();

  const [overrides, setOverrides] = useState<PendingOverride[]>([]);
  const [loading, setLoading] = useState(true);
  const [filterStatus, setFilterStatus] = useState<'all' | 'pending' | 'approved' | 'rejected'>('all');

  // Reject modal
  const [rejectModalOpen, setRejectModalOpen] = useState(false);
  const [rejectingItem, setRejectingItem] = useState<PendingOverride | null>(null);
  const [rejectReason, setRejectReason] = useState('');
  const [rejecting, setRejecting] = useState(false);

  // Approve state
  const [approvingId, setApprovingId] = useState<number | null>(null);

  const fetchOverrides = useCallback(async () => {
    setLoading(true);
    try {
      const res = await listPendingOverrides();
      const items = res.items || [];
      setOverrides(items);
      const pendingCount = items.filter((i) => i.status === 'pending').length;
      onPendingCountChange?.(pendingCount);
    } catch (e) {
      console.error('Failed to load overrides:', e);
    } finally {
      setLoading(false);
    }
  }, [onPendingCountChange]);

  useEffect(() => {
    fetchOverrides();
  }, [fetchOverrides]);

  const handleApprove = async (id: number) => {
    setApprovingId(id);
    try {
      await approveOverride(id);
      await fetchOverrides();
    } catch (e: any) {
      console.error('Failed to approve override:', e);
      alert(e.message || tr('审批通过失败', 'Failed to approve'));
    } finally {
      setApprovingId(null);
    }
  };

  const handleOpenReject = (item: PendingOverride) => {
    setRejectingItem(item);
    setRejectReason('');
    setRejectModalOpen(true);
  };

  const handleConfirmReject = async () => {
    if (!rejectingItem) return;
    if (!rejectReason.trim()) {
      alert(tr('请输入驳回原因', 'Please enter a rejection reason'));
      return;
    }
    setRejecting(true);
    try {
      await rejectOverride(rejectingItem.id);
      setRejectModalOpen(false);
      await fetchOverrides();
    } catch (e: any) {
      console.error('Failed to reject override:', e);
      alert(e.message || tr('驳回操作失败', 'Failed to reject'));
    } finally {
      setRejecting(false);
    }
  };

  const filteredOverrides = overrides.filter((item) => {
    if (filterStatus === 'all') return true;
    return item.status === filterStatus;
  });

  return (
    <div className="space-y-6">
      {/* Header & Filter */}
      <div className="flex flex-wrap items-center justify-between gap-4">
        <div>
          <h2 className="text-base font-semibold text-text">
            {tr('换班与代班审批 (Shift Swap & Overrides)', 'Shift Swap & Overrides Approvals')}
          </h2>
          <p className="text-xs text-text-muted mt-0.5">
            {tr(
              '工程师因突发事假或调休发起的代班/对调申请，由管理员或主管统一审批并更新值班日程。',
              'Review shift swaps and temporary override coverage requests submitted by engineers.'
            )}
          </p>
        </div>

        <div className="flex items-center gap-2">
          <div className="flex items-center rounded-lg border border-border bg-card p-0.5">
            <Button
              size="sm"
              variant={filterStatus === 'all' ? 'primary' : 'ghost'}
              onClick={() => setFilterStatus('all')}
            >
              {tr('全部', 'All')}
            </Button>
            <Button
              size="sm"
              variant={filterStatus === 'pending' ? 'primary' : 'ghost'}
              onClick={() => setFilterStatus('pending')}
            >
              {tr('待审批', 'Pending')}
            </Button>
            <Button
              size="sm"
              variant={filterStatus === 'approved' ? 'primary' : 'ghost'}
              onClick={() => setFilterStatus('approved')}
            >
              {tr('已通过', 'Approved')}
            </Button>
            <Button
              size="sm"
              variant={filterStatus === 'rejected' ? 'primary' : 'ghost'}
              onClick={() => setFilterStatus('rejected')}
            >
              {tr('已驳回', 'Rejected')}
            </Button>
          </div>

          <Button
            size="sm"
            variant="outline"
            onClick={fetchOverrides}
            disabled={loading}
            aria-label={tr('刷新', 'Refresh')}
            title={tr('刷新', 'Refresh')}
          >
            <RefreshCw size={14} className={loading ? 'animate-spin' : ''} />
          </Button>
        </div>
      </div>

      {/* Table Card */}
      <Card className="!p-0 overflow-hidden">
        {loading ? (
          <div className="flex h-64 items-center justify-center">
            <RefreshCw size={20} className="animate-spin text-indigo-500" />
          </div>
        ) : filteredOverrides.length === 0 ? (
          <EmptyState
            icon={UserCheck}
            title={tr('暂无换班申请记录', 'No Swap Requests')}
            hint={tr(
              '当工程师在排班大屏点击【申请换班/代班】时，相关审批单将显示在此处。',
              'Pending shift swap or override requests will appear here.'
            )}
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs border-collapse">
              <thead>
                <tr className="border-b border-border bg-bg/50 text-text-muted font-semibold">
                  <th className="py-3 px-4 w-16">ID</th>
                  <th className="py-3 px-4 w-44">{tr('排班计划', 'Schedule')}</th>
                  <th className="py-3 px-4 w-36">{tr('原值班人', 'Original')}</th>
                  <th className="py-3 px-4 w-36">{tr('代班人员', 'Substitute')}</th>
                  <th className="py-3 px-4 w-52">{tr('换班生效时段', 'Effective Window')}</th>
                  <th className="py-3 px-4">{tr('申请事由', 'Reason')}</th>
                  <th className="py-3 px-4 w-28 text-center">{tr('审批状态', 'Status')}</th>
                  <th className="py-3 px-4 w-36 text-left">{tr('操作', 'Actions')}</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-border">
                {filteredOverrides.map((item) => (
                  <tr key={item.id} className="hover:bg-bg/40 transition-colors">
                    <td className="py-3 px-4 font-mono text-text-faint">#{item.id}</td>
                    <td className="py-3 px-4 font-semibold text-text">
                      {item.schedule_name}
                    </td>
                    <td className="py-3 px-4 text-text">
                      {item.original_user_name || `User #${item.original_user_id}`}
                    </td>
                    <td className="py-3 px-4 text-indigo-600 dark:text-indigo-400 font-medium">
                      {item.substitute_user_name || `User #${item.substitute_user_id}`}
                    </td>
                    <td className="py-3 px-4 font-mono text-[11px] text-text-muted">
                      {new Date(item.start_time).toLocaleString()}<br />
                      ~ {new Date(item.end_time).toLocaleString()}
                    </td>
                    <td className="py-3 px-4 text-text-muted max-w-xs truncate" title={item.reason}>
                      {item.reason || '-'}
                    </td>
                    <td className="py-3 px-4 text-center">
                      <Chip
                        tone={
                          item.status === 'approved'
                            ? 'success'
                            : item.status === 'rejected'
                            ? 'danger'
                            : 'warning'
                        }
                        dense
                      >
                        {item.status === 'approved'
                          ? tr('已通过 (Approved)', 'Approved')
                          : item.status === 'rejected'
                          ? tr('已驳回 (Rejected)', 'Rejected')
                          : tr('待审批 (Pending)', 'Pending')}
                      </Chip>
                    </td>
                    <td className="py-3 px-4">
                      {item.status === 'pending' ? (
                        <div className="flex items-center gap-1.5">
                          <Button
                            size="sm"
                            variant="primary"
                            onClick={() => handleApprove(item.id)}
                            disabled={approvingId === item.id}
                          >
                            <Check size={12} className="mr-1" />
                            {approvingId === item.id ? tr('处理中', 'Saving') : tr('同意', 'Approve')}
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            className="text-red-600 hover:text-red-700 dark:text-red-400"
                            onClick={() => handleOpenReject(item)}
                          >
                            <X size={12} className="mr-1" />
                            {tr('驳回', 'Reject')}
                          </Button>
                        </div>
                      ) : (
                        <span className="text-text-faint text-[11px]">{tr('已结单', 'Completed')}</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {/* Reject Modal */}
      <Modal
        open={rejectModalOpen}
        onClose={() => setRejectModalOpen(false)}
        title={tr('驳回换班申请', 'Reject Swap Request')}
        size="sm"
        footer={
          <div className="flex justify-end gap-2 w-full">
            <Button size="sm" variant="ghost" onClick={() => setRejectModalOpen(false)}>
              {tr('取消', 'Cancel')}
            </Button>
            <Button
              size="sm"
              variant="danger"
              onClick={handleConfirmReject}
              disabled={rejecting || !rejectReason.trim()}
            >
              {rejecting ? tr('提交中...', 'Submitting...') : tr('确认驳回', 'Reject')}
            </Button>
          </div>
        }
      >
        <div className="space-y-3 py-2 text-xs">
          <p className="text-text-muted">
            {tr(
              `即将驳回 ${rejectingItem?.original_user_name} 申请由 ${rejectingItem?.substitute_user_name} 代班的单据，请输入驳回原因。`,
              `You are about to reject the override request. Please provide a reason.`
            )}
          </p>
          <div>
            <Label className="text-xs">{tr('驳回原因', 'Rejection Reason')} *</Label>
            <Input
              value={rejectReason}
              onChange={(e) => setRejectReason(e.target.value)}
              placeholder="e.g. 该时段涉及生产核心系统例行发布，禁止临时更换值班人员"
              className="mt-1 text-xs"
            />
          </div>
        </div>
      </Modal>
    </div>
  );
}
