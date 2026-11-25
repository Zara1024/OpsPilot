import { Component, type ErrorInfo, type ReactNode } from 'react';
import { AlertTriangle } from 'lucide-react';
import { Button } from '@/components/ui';
import { cn } from '@/lib/cn';
import { tr } from '@/i18n/locale';

type ErrorBoundaryProps = {
  children: ReactNode;
  resetKey?: string;
  fullScreen?: boolean;
};

type ErrorBoundaryState = {
  error: Error | null;
};

export class ErrorBoundary extends Component<ErrorBoundaryProps, ErrorBoundaryState> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info);
  }

  componentDidUpdate(prevProps: ErrorBoundaryProps) {
    if (this.state.error && prevProps.resetKey !== this.props.resetKey) {
      this.setState({ error: null });
    }
  }

  private handleRetry = () => {
    this.setState({ error: null });
  };

  private handleGoHome = () => {
    window.location.assign('/');
  };

  render() {
    const { error } = this.state;
    if (!error) return this.props.children;

    return (
      <div
        role="alert"
        className={cn(
          'flex w-full items-center justify-center bg-bg p-6 text-text',
          this.props.fullScreen ? 'min-h-screen' : 'min-h-full flex-1',
        )}
      >
        <div className="w-full max-w-md rounded-xl border border-border bg-card p-6 shadow-sm">
          <div className="flex items-start gap-3">
            <div className="mt-0.5 rounded-lg bg-red-500/10 p-2 text-red-500">
              <AlertTriangle size={18} aria-hidden="true" />
            </div>
            <div className="min-w-0 flex-1 space-y-1.5">
              <h2 className="text-sm font-semibold text-text">
                {tr('页面渲染出现异常', 'Something went wrong')}
              </h2>
              <p className="text-xs leading-relaxed text-text-muted">
                {tr(
                  '当前视图发生未预期错误，你可以尝试重新加载此区域或返回首页。',
                  'An unexpected error occurred in this view. Retry this section or return home.',
                )}
              </p>
              {error.message && (
                <p className="break-words rounded-md border border-border-soft bg-bg px-2.5 py-2 font-mono text-xs text-text-faint">
                  {error.message}
                </p>
              )}
              <div className="flex flex-wrap items-center gap-2 pt-2">
                <Button variant="primary" size="sm" onClick={this.handleRetry}>
                  {tr('重试', 'Retry')}
                </Button>
                <Button variant="outline" size="sm" onClick={this.handleGoHome}>
                  {tr('返回首页', 'Back to home')}
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>
    );
  }
}
